package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func FuzzSanitizeArchivePath(f *testing.F) {
	seeds := []string{
		"foo/bar.txt",
		"../escape",
		"foo/../../bar",
		"a/../b",
		"normal/name",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, path string) {
		got, err := sanitizeArchivePath(path)
		if err != nil {
			return
		}

		if strings.Contains(got, "..") {
			t.Fatalf("result contains ..: %q", got)
		}
		if filepath.IsAbs(got) {
			t.Fatalf("result is absolute: %q", got)
		}

		got2, err2 := sanitizeArchivePath(got)
		if err2 != nil {
			t.Fatalf("idempotent sanitize failed: %v", err2)
		}
		if got2 != got {
			t.Fatalf("not idempotent: %q -> %q", got, got2)
		}
	})
}
