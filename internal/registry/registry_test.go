package registry

import (
	"strings"
	"testing"
)

// The mirror must trust the guest bundle instead of its own image's store, not
// in addition to it: mounting over the image's bundle alone leaves the roots
// in the image's /etc/ssl/certs trusted (measured: 149 → 157 roots, a removed
// root still present).
func TestCATrustArgsReplacesImageStore(t *testing.T) {
	args := caTrustArgs()
	for _, want := range []string{
		"-v /etc/ssl/certs/ca-certificates.crt:/etc/marina/ca-certificates.crt:ro",
		"-e SSL_CERT_FILE=/etc/marina/ca-certificates.crt",
		"-e SSL_CERT_DIR=/etc/marina",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("caTrustArgs() = %q, missing %q", args, want)
		}
	}
	if strings.Contains(args, ":/etc/ssl/certs/") {
		t.Errorf("caTrustArgs() = %q mounts into the image's /etc/ssl/certs", args)
	}
}
