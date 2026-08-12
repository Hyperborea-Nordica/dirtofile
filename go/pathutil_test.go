package main

import (
	"path/filepath"
	"testing"
)

func TestSanitizeArchivePath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"a/b/c.txt", "a/b/c.txt", false},
		{"../secret", "", true},
		{"foo/../../bar", "", true},
	}

	for _, tc := range cases {
		got, err := sanitizeArchivePath(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("sanitizeArchivePath(%q) expected error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("sanitizeArchivePath(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("sanitizeArchivePath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestValidateCustomPathChars(t *testing.T) {
	t.Parallel()

	if err := validateCustomPathChars("ok.txt"); err != nil {
		t.Fatal(err)
	}
	if err := validateCustomPathChars("bad\nname"); err == nil {
		t.Fatal("expected error for newline in path")
	}
}

func TestRelDepth(t *testing.T) {
	t.Parallel()

	if relDepth(".") != 0 {
		t.Fatal("root depth should be 0")
	}
	if relDepth(filepath.Join("a", "b.txt")) != 1 {
		t.Fatal("expected depth 1")
	}
}

func TestIsUnderRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	inside := filepath.Join(root, "a", "b.txt")
	outside := filepath.Join(root, "..", "outside.txt")

	if !isUnderRoot(inside, root) {
		t.Fatal("expected inside path to be under root")
	}
	if isUnderRoot(outside, root) {
		t.Fatal("expected outside path to be rejected")
	}
}
