package cli

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/bcollard/marina/internal/guest"
)

// checkIDOutbound is the VM's own internet access — what dockerd uses for
// `docker pull` and `docker login`, and the mirrors for their upstreams.
const checkIDOutbound = "outbound"

// fallbackMetric is the metric of the default route `--fix` adds via eth0. It
// sits below lima0's DHCP route (100) so it wins, and is what marks the route
// as marina's when the check runs again.
const fallbackMetric = 50

// outboundProbeURL is answered with a 401 when reached; any HTTP status counts
// as connectivity. curl reads the proxy from the SSH session's environment
// (pam_env applies /etc/environment), so a proxied VM is probed through it.
const outboundProbeURL = "https://registry-1.docker.io/v2/"

// outboundProbeScript probes the default path and, only when that fails, the
// eth0 path. IPv4 only: the VM also gets IPv6 through lima0 (macOS NAT66),
// which can keep working while IPv4 does not, and the eth0 fallback route is
// IPv4. It never uses ping: macOS drops ICMP to bridge100 even when the lima0
// path forwards TCP fine.
var outboundProbeScript = fmt.Sprintf(`probe() { curl -4 -sS -o /dev/null --connect-timeout 3 --max-time 6 -w '%%{http_code}' "$@" %s 2>/dev/null || true; }
d=$(probe)
echo "default=$d"
echo "dev=$(ip -4 route get 1.1.1.1 2>/dev/null | awk '{for(i=1;i<NF;i++) if($i=="dev"){print $(i+1); exit}}')"
echo "eth0gw=$(ip -4 route show default dev eth0 2>/dev/null | awk '/via/{print $3; exit}')"
echo "fallback=$(ip -4 route show default 2>/dev/null | grep -c 'dev eth0 metric %d')"
if [ "$d" = "000" ] || [ -z "$d" ]; then echo "eth0=$(probe --interface eth0)"; fi
`, outboundProbeURL, fallbackMetric)

// outboundProbe is the parsed output of outboundProbeScript.
type outboundProbe struct {
	defaultOK bool   // the default path reached the probe URL
	eth0OK    bool   // eth0 did (only probed when the default path failed)
	dev       string // interface the default route uses
	eth0GW    string // eth0's DHCP gateway (Lima's user-mode network)
	fallback  bool   // marina's fallback route is installed
}

// reached reports whether curl got an HTTP status back. "000" is curl's code
// for "no response at all".
func reached(code string) bool {
	return code != "" && code != "000"
}

func parseOutboundProbe(out string) outboundProbe {
	var p outboundProbe
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch k {
		case "default":
			p.defaultOK = reached(v)
		case "eth0":
			p.eth0OK = reached(v)
		case "dev":
			p.dev = v
		case "eth0gw":
			if net.ParseIP(v) != nil {
				p.eth0GW = v
			}
		case "fallback":
			p.fallback = v != "" && v != "0"
		}
	}
	return p
}

// outboundCheck turns a probe into a check. Split from the guest call so every
// branch is testable.
func outboundCheck(p outboundProbe) doctorCheck {
	restart := "marina down -c " + configFile + " && marina up -c " + configFile
	switch {
	case p.defaultOK && p.fallback && p.dev == "eth0":
		return doctorCheck{ID: checkIDOutbound, Status: checkWarn,
			Message: "VM reaches the internet through marina's fallback route on eth0, not lima0",
			Detail:  "The route was added by 'marina doctor --fix' and lasts until the VM restarts.",
			Fix:     restart + "   (restores the lima0 path)"}
	case p.defaultOK:
		via := ""
		if p.dev != "" {
			via = " (via " + p.dev + ")"
		}
		return doctorCheck{ID: checkIDOutbound, Status: checkOK,
			Message: "VM reaches the internet" + via}
	case p.eth0OK && p.eth0GW != "":
		return doctorCheck{ID: checkIDOutbound, Status: checkFail,
			Message: "VM cannot reach the internet through lima0; eth0 (Lima user-mode network) can",
			Detail: "macOS stopped forwarding the VM's traffic on its vzNAT bridge. This usually follows a " +
				"Wi-Fi or VPN change, or sleep, while the VM runs; host firewall or EDR software filtering " +
				"the bridge does the same. docker pull, docker login and the registry mirrors all fail.",
			Fix:     restart + "   (--fix adds a default route via eth0 instead; it lasts until the next restart)",
			Fixable: true}
	default:
		return doctorCheck{ID: checkIDOutbound, Status: checkFail,
			Message: "VM cannot reach the internet on any interface",
			Detail: "Neither lima0 nor eth0 reached " + outboundProbeURL + ". Check the Mac's own " +
				"connection; behind a proxy, check network.proxy (see the proxy check).",
			Fix: restart}
	}
}

func checkOutbound(ctx context.Context, g *guest.Client) doctorCheck {
	out, err := g.Run(ctx, outboundProbeScript)
	if err != nil {
		return doctorCheck{ID: checkIDOutbound, Status: checkWarn,
			Message: fmt.Sprintf("Cannot check the VM's internet access: %v", err)}
	}
	return outboundCheck(parseOutboundProbe(out))
}

// fixOutbound routes the VM's outbound traffic through eth0, then probes again
// so a route that does not help is reported as a failed fix.
func fixOutbound(ctx context.Context, g *guest.Client) error {
	out, err := g.Run(ctx, outboundProbeScript)
	if err != nil {
		return err
	}
	p := parseOutboundProbe(out)
	if p.eth0GW == "" {
		return fmt.Errorf("eth0 has no default gateway")
	}
	if _, err := g.Run(ctx, fmt.Sprintf("sudo ip route replace default via %s dev eth0 metric %d", p.eth0GW, fallbackMetric)); err != nil {
		return fmt.Errorf("adding the eth0 route: %w", err)
	}
	out, err = g.Run(ctx, outboundProbeScript)
	if err != nil {
		return err
	}
	if !parseOutboundProbe(out).defaultOK {
		return fmt.Errorf("the VM still cannot reach the internet through eth0")
	}
	return nil
}
