package routing

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os/exec"
	"strings"

	"github.com/bcollard/marina/internal/guest"
)

// EnsureRoute idempotently adds a macOS route for cidr via the VM's lima0 IP.
// If a route for that CIDR already points at lima0IP it is a no-op — importantly
// it does NOT invoke sudo, so re-running `marina up` against an already-running VM
// never prompts for a password. Only a missing or stale (wrong-gateway) route
// triggers the sudo delete+add.
//
// interactive=false (no TTY: launchd autostart, CI, an agent's shell) runs
// `sudo -n`, which succeeds through the `marina sudoers` NOPASSWD rules (-n is a
// sudo option, so the rules still match) and otherwise fails at once. Plain sudo
// there waits on a password or Touch ID prompt nobody answers, and `up` hangs.
func EnsureRoute(cidr, lima0IP string, interactive bool) error {
	if gw, ok := RouteGateway(cidr); ok {
		if gw == lima0IP {
			slog.Info("macOS route already present and correct — skipping", "cidr", cidr, "via", lima0IP)
			return nil
		}
		slog.Info("macOS route has a stale gateway — refreshing", "cidr", cidr, "old", gw, "new", lima0IP)
	}

	slog.Info("Ensuring macOS route", "cidr", cidr, "via", lima0IP)

	// Best-effort delete of any stale route.
	del := sudoRoute(interactive, "-n", "delete", "-net", cidr)
	_ = del.Run() // ignore errors (route may not exist)

	add := sudoRoute(interactive, "-n", "add", "-net", cidr, lima0IP)
	slog.Debug("route add", "cmd", strings.Join(add.Args, " "))
	if out, err := add.CombinedOutput(); err != nil {
		if !interactive {
			return fmt.Errorf("adding route %s via %s: %w\n%s(no terminal to ask for a password: install the NOPASSWD rules printed by `marina sudoers`, or run interactively)", cidr, lima0IP, err, out)
		}
		return fmt.Errorf("adding route %s via %s: %w\n%s", cidr, lima0IP, err, out)
	}
	return nil
}

// sudoRoute builds `sudo [-n] /sbin/route <args>`. The /sbin/route arguments
// must stay exactly what `marina sudoers` emits, or the NOPASSWD rules miss.
func sudoRoute(interactive bool, routeArgs ...string) *exec.Cmd {
	return exec.Command("sudo", sudoRouteArgs(interactive, routeArgs...)...)
}

func sudoRouteArgs(interactive bool, routeArgs ...string) []string {
	args := []string{}
	if !interactive {
		args = append(args, "-n")
	}
	return append(append(args, "/sbin/route"), routeArgs...)
}

// RouteGateway returns the gateway of the macOS route that currently serves the
// given CIDR, and whether a route dedicated to that CIDR exists. It returns
// ok=false when only a broader/default route matches, so the caller knows a
// dedicated route still needs to be added. It performs no privileged operations
// (no sudo), making it safe to call on every `marina up`.
func RouteGateway(cidr string) (string, bool) {
	ip, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", false
	}
	base := ip.Mask(ipNet.Mask)
	probe := make(net.IP, len(base))
	copy(probe, base)
	probe[len(probe)-1]++

	out, err := exec.Command("/sbin/route", "-n", "get", probe.String()).CombinedOutput()
	if err != nil {
		return "", false
	}
	var dest, gw string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "destination:"):
			dest = strings.TrimSpace(strings.TrimPrefix(line, "destination:"))
		case strings.HasPrefix(line, "gateway:"):
			gw = strings.TrimSpace(strings.TrimPrefix(line, "gateway:"))
		}
	}
	// Only "our" route if the matched destination is the CIDR base (not the
	// default route or a broader aggregate).
	if dest != base.String() || gw == "" {
		return "", false
	}
	return gw, true
}

// DeleteRoute removes the macOS route for cidr. Best-effort; ignores "not found".
// interactive has the same meaning as in EnsureRoute.
func DeleteRoute(cidr string, interactive bool) error {
	slog.Info("Deleting macOS route", "cidr", cidr)
	del := sudoRoute(interactive, "-n", "delete", "-net", cidr)
	if out, err := del.CombinedOutput(); err != nil {
		slog.Warn("route delete failed (may not exist)", "cidr", cidr, "err", err, "output", string(out))
	}
	return nil
}

// RouteExists checks whether a dedicated macOS route for the given CIDR exists.
// It delegates to RouteGateway so that a broader match — most importantly the
// default route, which answers `route -n get` for any address — is not mistaken
// for our route.
func RouteExists(cidr string) bool {
	_, ok := RouteGateway(cidr)
	return ok
}

// Lima0IP returns the IPv4 address of the lima0 interface inside the VM.
// This is the gateway the macOS host must route kind traffic through.
func Lima0IP(ctx context.Context, g *guest.Client) (string, error) {
	out, err := g.Run(ctx, `ip -o -4 addr show lima0 | awk '{print $4}' | cut -d/ -f1`)
	if err != nil {
		return "", fmt.Errorf("detecting lima0 IP: %w", err)
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return "", fmt.Errorf("lima0 interface has no IPv4 address")
	}
	if ip := net.ParseIP(out); ip == nil {
		return "", fmt.Errorf("invalid IP from lima0: %q", out)
	}
	return out, nil
}
