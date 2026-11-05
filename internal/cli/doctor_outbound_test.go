package cli

import "testing"

func TestParseOutboundProbe(t *testing.T) {
	p := parseOutboundProbe("default=000\ndev=lima0\neth0gw=192.168.5.2\nfallback=0\neth0=401\n")
	want := outboundProbe{eth0OK: true, dev: "lima0", eth0GW: "192.168.5.2"}
	if p != want {
		t.Errorf("got %+v, want %+v", p, want)
	}
	if p := parseOutboundProbe("default=\neth0gw=not-an-ip\nfallback=1\n"); p.defaultOK || p.eth0GW != "" || !p.fallback {
		t.Errorf("got %+v", p)
	}
}

func TestOutboundCheck(t *testing.T) {
	tests := []struct {
		name    string
		probe   outboundProbe
		status  string
		fixable bool
	}{
		{"healthy", outboundProbe{defaultOK: true, dev: "lima0"}, checkOK, false},
		{"fallback route active", outboundProbe{defaultOK: true, dev: "eth0", fallback: true}, checkWarn, false},
		{"lima0 broken, eth0 works", outboundProbe{eth0OK: true, eth0GW: "192.168.5.2"}, checkFail, true},
		{"eth0 works but no gateway", outboundProbe{eth0OK: true}, checkFail, false},
		{"nothing works", outboundProbe{eth0GW: "192.168.5.2"}, checkFail, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := outboundCheck(tt.probe)
			if c.ID != checkIDOutbound || c.Status != tt.status || c.Fixable != tt.fixable {
				t.Errorf("got id=%s status=%s fixable=%v, want status=%s fixable=%v", c.ID, c.Status, c.Fixable, tt.status, tt.fixable)
			}
		})
	}
}
