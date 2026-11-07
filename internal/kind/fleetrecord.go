package kind

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/bcollard/marina/internal/guest"
)

// FleetRecordName is the kube-system ConfigMap in which a fleet member keeps
// the manifest it was created (or adopted) from.
//
// The manifest itself is ephemeral input, like `kubectl apply -f`, and the
// options it set beyond labels (dependsOn, registries, addons) cannot be
// recovered from live state. Keeping a copy in the cluster it describes means
// it lives and dies with that cluster: no host-side state to go stale.
const FleetRecordName = "marina-fleet"

// FleetRecord is the content of the marina-fleet ConfigMap.
type FleetRecord struct {
	Fleet     string `json:"fleet"`
	AppliedAt string `json:"appliedAt"` // RFC 3339
	// Manifest is the whole Fleet manifest, as applied.
	Manifest string `json:"manifest.yaml"`
	// Cluster is this member's resolved entry (defaults merged, num set), YAML.
	Cluster string `json:"cluster.yaml"`
}

// WriteFleetRecord creates or replaces the cluster's marina-fleet ConfigMap.
func WriteFleetRecord(ctx context.Context, g *guest.Client, clusterName string, rec FleetRecord) error {
	if rec.AppliedAt == "" {
		rec.AppliedAt = time.Now().UTC().Format(time.RFC3339)
	}
	b64 := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
	// Content travels base64-encoded: manifests carry quotes, `$` and newlines
	// that would otherwise need shell escaping.
	script := fmt.Sprintf(`#!/bin/bash
set -euo pipefail
%s
d=$(mktemp -d)
trap 'rm -rf "$d"' EXIT
echo '%s' | base64 -d > "$d/manifest.yaml"
echo '%s' | base64 -d > "$d/cluster.yaml"
kubectl --kubeconfig "$kc" -n kube-system create configmap %s \
  --from-literal=fleet='%s' --from-literal=appliedAt='%s' \
  --from-file=manifest.yaml="$d/manifest.yaml" --from-file=cluster.yaml="$d/cluster.yaml" \
  --dry-run=client -o yaml \
  | kubectl --kubeconfig "$kc" label --local -f - managed-by=marina -o yaml \
  | kubectl --kubeconfig "$kc" apply -f - >/dev/null
`, guest.KindKubeconfig("kc", clusterName), b64(rec.Manifest), b64(rec.Cluster), FleetRecordName, rec.Fleet, rec.AppliedAt)
	return g.RunScript(ctx, fmt.Sprintf("record fleet manifest on cluster %q", clusterName), script)
}

// AddonStatus is the live state of an addon, detected from the cluster rather
// than from the manifest, so it is also right for clusters created before
// marina kept a FleetRecord, or with the addon installed some other way.
type AddonStatus struct {
	Installed bool   `json:"installed"           yaml:"installed"`
	Version   string `json:"version,omitempty"   yaml:"version,omitempty"`
	Ready     bool   `json:"ready"               yaml:"ready"`
}

// ClusterExtras is what `fleet describe` reads beyond ClusterInfo.
type ClusterExtras struct {
	Record        *FleetRecord `json:"record"`
	MetricsServer *AddonStatus `json:"metricsServer"`
}

// ClusterExtrasFor reads the marina-fleet ConfigMap and the metrics-server
// Deployment in one guest round trip. An unreachable cluster yields empty extras.
func ClusterExtrasFor(ctx context.Context, g *guest.Client, clusterName string) (*ClusterExtras, error) {
	cmd := fmt.Sprintf(`%s 2>/dev/null
cm=$(kubectl --kubeconfig "$kc" -n kube-system get configmap %s -o json 2>/dev/null || echo null)
ms=$(kubectl --kubeconfig "$kc" -n kube-system get deployment metrics-server -o json 2>/dev/null || echo null)
jq -cn --argjson cm "$cm" --argjson ms "$ms" '{
  record: (if $cm == null then null else $cm.data end),
  metricsServer: (if $ms == null then null else {
    installed: true,
    version: ($ms.spec.template.spec.containers[0].image | split(":") | last),
    ready: (($ms.status.readyReplicas // 0) >= ($ms.spec.replicas // 1))
  } end)
}'`, guest.KindKubeconfig("kc", clusterName), FleetRecordName)
	out, err := g.Run(ctx, cmd)
	if err != nil {
		return nil, fmt.Errorf("reading cluster %q fleet record: %w", clusterName, err)
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return &ClusterExtras{}, nil
	}
	var ex ClusterExtras
	if err := json.Unmarshal([]byte(out), &ex); err != nil {
		return nil, fmt.Errorf("parsing cluster %q fleet record: %w", clusterName, err)
	}
	return &ex, nil
}
