//go:build unix

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUnreadableFileFailsWalk(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can read all files")
	}

	dir := t.TempDir()
	secret := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(secret, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(secret, 0o600) })

	_ = os.WriteFile(filepath.Join(dir, "ok.txt"), []byte("ok"), 0o644)

	out := filepath.Join(t.TempDir(), "out.tar")
	cfg := &Config{
		Directory: dir,
		Output:    out,
		Format:    "tar",
		Recursive: false,
		Force:     true,
		Quiet:     true,
	}

	if _, err := pack(cfg); err == nil {
		t.Fatal("expected error when a file is not readable")
	}
}
