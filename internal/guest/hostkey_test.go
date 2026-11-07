package guest

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestSetHostKeyCheck(t *testing.T) {
	t.Run("no pinned key", func(t *testing.T) {
		cfg := &ssh.ClientConfig{}
		if err := setHostKeyCheck(cfg, t.TempDir()); err != nil {
			t.Fatal(err)
		}
		if cfg.HostKeyCallback == nil || cfg.HostKeyAlgorithms != nil {
			t.Fatalf("want an unrestricted callback, got algos %v", cfg.HostKeyAlgorithms)
		}
	})

	t.Run("pinned ed25519 key", func(t *testing.T) {
		pub, _, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		key, err := ssh.NewPublicKey(pub)
		if err != nil {
			t.Fatal(err)
		}
		other, _, _ := ed25519.GenerateKey(rand.Reader)
		otherKey, _ := ssh.NewPublicKey(other)

		dir := t.TempDir()
		line := "lima-test " + string(ssh.MarshalAuthorizedKey(key))
		if err := os.WriteFile(filepath.Join(dir, "ssh.known_hosts"), []byte(line), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg := &ssh.ClientConfig{}
		if err := setHostKeyCheck(cfg, dir); err != nil {
			t.Fatal(err)
		}
		if len(cfg.HostKeyAlgorithms) != 1 || cfg.HostKeyAlgorithms[0] != ssh.KeyAlgoED25519 {
			t.Fatalf("HostKeyAlgorithms = %v", cfg.HostKeyAlgorithms)
		}
		if err := cfg.HostKeyCallback("x", nil, key); err != nil {
			t.Fatalf("pinned key rejected: %v", err)
		}
		if err := cfg.HostKeyCallback("x", nil, otherKey); err == nil {
			t.Fatal("a different key was accepted")
		}
	})
}
