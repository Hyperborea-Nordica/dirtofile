//go:build unix

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSymlinkOutsideRootSkippedWithoutFollow(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()

	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "safe.txt"), []byte("safe"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "evil-link")
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), link); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "out.tar")
	cfg := &Config{
		Directory:   dir,
		Output:      out,
		Format:      "tar",
		Recursive:   true,
		Force:       true,
		Quiet:       true,
		FollowLinks: false,
	}

	count, err := pack(cfg)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 file, got %d", count)
	}

	names, err := listTarNames(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "safe.txt" {
		t.Fatalf("unexpected archive contents: %v", names)
	}
}

func TestSymlinkOutsideRootSkippedWithFollow(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()

	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "safe.txt"), []byte("safe"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "evil-link")
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), link); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "out.tar")
	cfg := &Config{
		Directory:   dir,
		Output:      out,
		Format:      "tar",
		Recursive:   true,
		Force:       true,
		Quiet:       true,
		FollowLinks: true,
	}

	count, err := pack(cfg)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 file with follow-links to outside target, got %d", count)
	}

	names, err := listTarNames(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if n == "secret.txt" || n == "evil-link" {
			t.Fatalf("outside symlink must not be archived, got %v", names)
		}
	}
}
