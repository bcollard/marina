package limatemplate

import (
	"testing"

	"github.com/bcollard/marina/internal/config"
)

// Lima must not mount the image disk itself: its mount runs in cloud-final,
// after Docker, and marina's own early unit mounts it at /var/lib/containerd.
func TestImageDiskNotMountedByLima(t *testing.T) {
	cfg := &config.Config{}
	cfg.VM.Name = "marina"
	cfg.VM.ImageDisk = "30GiB"
	cfg.Registries.CacheStorage = "guest"

	y := Build(cfg)
	if len(y.AdditionalDisks) != 1 {
		t.Fatalf("got %d additional disks, want 1", len(y.AdditionalDisks))
	}
	d := y.AdditionalDisks[0]
	if d.Mount == nil || *d.Mount {
		t.Error("image disk must be attached with mount: false")
	}
	if d.MountPoint != nil {
		t.Errorf("mountPoint = %q, want unset", *d.MountPoint)
	}
	if d.Format == nil || !*d.Format {
		t.Error("Lima must still format the disk on first attach")
	}
}
