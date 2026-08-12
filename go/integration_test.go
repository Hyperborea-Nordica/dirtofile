package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNonRecursivePackOnlyRoot(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "root.txt"), []byte("root"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "nested.txt"), []byte("nested"), 0o644); err != nil {
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
		t.Fatalf("pack: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 file, got %d", count)
	}

	names, err := listTarNames(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "root.txt" {
		t.Fatalf("unexpected tar entries: %v", names)
	}
}

func TestValidateOutputRejectsExisting(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "exists.tar")
	if err := os.WriteFile(existing, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &Config{
		Directory: dir,
		Output:    existing,
		Force:     false,
	}
	if err := validateOutput(cfg); err == nil {
		t.Fatal("expected error when output exists without --force")
	}
}

func TestMaxFilesLimit(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0o644)

	out := filepath.Join(t.TempDir(), "out.tar")
	cfg := &Config{
		Directory: dir,
		Output:    out,
		Format:    "tar",
		Force:     true,
		Quiet:     true,
		MaxFiles:  1,
	}

	if _, err := pack(cfg); err == nil {
		t.Fatal("expected max-files error")
	}
}
