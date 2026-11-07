package vm

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveStaleSocket(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "sock") // short path: sun_path is 104 bytes on macOS
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	sock := filepath.Join(dir, "d.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	l.(*net.UnixListener).SetUnlinkOnClose(false) // leave it behind, like a killed hostagent
	l.Close()

	removeStaleSocket(sock)
	if _, err := os.Lstat(sock); !os.IsNotExist(err) {
		t.Fatalf("socket not removed: %v", err)
	}

	regular := filepath.Join(dir, "regular")
	if err := os.WriteFile(regular, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	removeStaleSocket(regular)
	if _, err := os.Lstat(regular); err != nil {
		t.Fatalf("regular file removed: %v", err)
	}
}
