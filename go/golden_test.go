package main

import (
	"path/filepath"
	"testing"
)

func TestGoldenRecursiveTarListing(t *testing.T) {
	fixture := testdataPath("fixtures", "standard")
	expected, err := loadGoldenLines("standard-recursive.txt")
	if err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "out.tar")
	cfg := &Config{
		Directory: fixture,
		Output:    out,
		Format:    "tar",
		Recursive: true,
		Force:     true,
		Quiet:     true,
	}

	count, err := pack(cfg)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	if count != len(expected) {
		t.Fatalf("file count: got %d, want %d", count, len(expected))
	}

	got, err := listTarNames(out)
	if err != nil {
		t.Fatal(err)
	}
	assertEqualStrings(t, got, expected, "tar listing")
}

func TestGoldenFlatTarListing(t *testing.T) {
	fixture := testdataPath("fixtures", "standard")
	expected, err := loadGoldenLines("standard-flat.txt")
	if err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "out.tar")
	cfg := &Config{
		Directory: fixture,
		Output:    out,
		Format:    "tar",
		Recursive: false,
		Force:     true,
		Quiet:     true,
	}

	_, err = pack(cfg)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}

	got, err := listTarNames(out)
	if err != nil {
		t.Fatal(err)
	}
	assertEqualStrings(t, got, expected, "tar listing")
}

func assertEqualStrings(t *testing.T, got, want []string, label string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: len got %d want %d\ngot:  %v\nwant: %v", label, len(got), len(want), got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s[%d]: got %q want %q", label, i, got[i], want[i])
		}
	}
}
