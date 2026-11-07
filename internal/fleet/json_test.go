package fleet

import (
	"encoding/json"
	"strings"
	"testing"
)

// `fleet describe -o json` exposes ClusterEntry to marina-ui: keys must match
// the manifest's (camelCase), and an explicit "no mirrors" must survive.
func TestClusterEntryJSON(t *testing.T) {
	none := []string{}
	e := ClusterEntry{Name: "b", DependsOn: []string{"a"}, Registries: &RegistrySelect{Mirrors: &none},
		Addons: &Addons{MetricsServer: &MetricsServer{Enabled: true}}}
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, want := range []string{`"dependsOn":["a"]`, `"registries":{"mirrors":[]}`, `"metricsServer":{"enabled":true}`} {
		if !strings.Contains(got, want) {
			t.Errorf("%s missing %s", got, want)
		}
	}
}
