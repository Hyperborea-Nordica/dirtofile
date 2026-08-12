//go:build slow

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLargeFileStreaming(t *testing.T) {
	dir := t.TempDir()
	payload := make([]byte, 5*1024*1024)
	for i := range payload {
		payload[i] = byte(i % 251)
	}
	if err := os.WriteFile(filepath.Join(dir, "large.bin"), payload, 0o644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "out.tar")
	cfg := &Config{
		Directory: dir,
		Output:    out,
		Format:    "tar",
		Force:     true,
		Quiet:     true,
	}

	count, err := pack(cfg)
	if err != nil {
		t.Fatalf("pack large file: %v", err)
	}
	if count != 1 {
		t.Fatalf("want 1 file, got %d", count)
	}

	info, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() < 1024 {
		t.Fatal("archive suspiciously small")
	}
}
