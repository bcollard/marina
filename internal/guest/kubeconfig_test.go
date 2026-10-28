package guest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Runs the generated shell with a stub `kind`, the way the guest does.
func TestKindKubeconfigPerUserPrivateFile(t *testing.T) {
	stubs := t.TempDir()
	kind := "#!/bin/sh\necho 'server: https://0.0.0.0:7001'\n"
	if err := os.WriteFile(filepath.Join(stubs, "kind"), []byte(kind), 0o755); err != nil {
		t.Fatal(err)
	}
	script := KindKubeconfig("kc", "zz-test") + "\nprintf '%s' \"$kc\""
	cmd := exec.Command("bash", "-c", script)
	cmd.Env = append(os.Environ(), "PATH="+stubs+":"+os.Getenv("PATH"))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("script failed: %v", err)
	}
	path := string(out)
	t.Cleanup(func() { os.Remove(path) })

	if want := fmt.Sprintf("/tmp/marina-kube-%d-zz-test.yaml", os.Getuid()); path != want {
		t.Errorf("path = %q, want %q (per-user, so root and lima never share a file)", path, want)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := fi.Mode().Perm(); mode != 0o600 {
		t.Errorf("mode = %o, want 600 (admin credentials)", mode)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "https://127.0.0.1:7001") {
		t.Errorf("server not rewritten to 127.0.0.1: %q", b)
	}
}
