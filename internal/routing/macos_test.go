package routing

import (
	"reflect"
	"testing"
)

func TestSudoRouteArgs(t *testing.T) {
	// The /sbin/route part must stay byte-identical across modes so the
	// `marina sudoers` NOPASSWD rules match; only sudo's own -n is added.
	got := sudoRouteArgs(false, "-n", "add", "-net", "172.30.0.0/16", "192.168.64.2")
	want := []string{"-n", "/sbin/route", "-n", "add", "-net", "172.30.0.0/16", "192.168.64.2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("non-interactive: got %v, want %v", got, want)
	}
	got = sudoRouteArgs(true, "-n", "delete", "-net", "172.30.0.0/16")
	want = []string{"/sbin/route", "-n", "delete", "-net", "172.30.0.0/16"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("interactive: got %v, want %v", got, want)
	}
}
