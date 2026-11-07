package cli

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/bcollard/marina/internal/config"
	"github.com/bcollard/marina/internal/guest"
	"github.com/bcollard/marina/internal/limatemplate"
)

// reconcileKindCLI makes the VM's kind binary match limatemplate.KindCLIVersion.
//
// The provision script installs kind only when it is missing, and it is baked
// into the instance at creation, so a marina upgrade that bumps kind would
// otherwise never reach an existing VM: new clusters would be created with the
// old CLI from node images built for the new one. Replacing the binary does not
// touch running clusters — kind keeps no state outside the node containers.
//
// It reports whether the binary was replaced.
func reconcileKindCLI(ctx context.Context, g *guest.Client) (bool, error) {
	out, err := g.Run(ctx, "kind version 2>/dev/null || true")
	if err != nil {
		return false, fmt.Errorf("reading the kind version: %w", err)
	}
	have := parseKindVersion(out)
	if have == limatemplate.KindCLIVersion {
		return false, nil
	}
	slog.Info("Updating the kind CLI in the VM", "from", have, "to", limatemplate.KindCLIVersion)
	script := fmt.Sprintf(`#!/bin/bash
set -euo pipefail
# The download may need the proxy Lima/marina wrote into /etc/environment.
if [ -f /etc/environment ]; then set -a; . /etc/environment; set +a; fi
ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')
tmp=$(mktemp /usr/local/bin/.kind.XXXXXX)
trap 'rm -f "$tmp"' EXIT
curl -fsSLo "$tmp" "https://kind.sigs.k8s.io/dl/%s/kind-linux-${ARCH}"
chmod 0755 "$tmp"
"$tmp" version >/dev/null
mv -f "$tmp" /usr/local/bin/kind
`, limatemplate.KindCLIVersion)
	if err := g.RunScript(ctx, "update kind CLI", script); err != nil {
		return false, fmt.Errorf("installing kind %s: %w", limatemplate.KindCLIVersion, err)
	}
	return true, nil
}

// parseKindVersion extracts "v0.33.0" from `kind version` output
// ("kind v0.33.0 go1.26.3 linux/arm64"). Empty when kind is missing.
func parseKindVersion(out string) string {
	f := strings.Fields(out)
	if len(f) >= 2 && f[0] == "kind" {
		return f[1]
	}
	return ""
}

// warnPinnedNodeVersion is the after-upgrade counterpart of the nodeVersion
// review at VM creation: once the VM runs a newer kind, a config that pins the
// previous default keeps creating clusters from an image the new CLI was not
// validated against.
func warnPinnedNodeVersion(cfg *config.Config) error {
	if cfg.Kind.NodeVersion == config.DefaultKindNodeVersion {
		return nil
	}
	return offerNodeVersionUpdate(cfg, "before creating new clusters")
}
