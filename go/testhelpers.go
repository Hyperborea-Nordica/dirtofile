package main

import (
	"archive/tar"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

func repoRoot() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ".."
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), ".."))
}

func testdataPath(parts ...string) string {
	all := append([]string{repoRoot(), "testdata"}, parts...)
	return filepath.Join(all...)
}

func loadGoldenLines(rel string) ([]string, error) {
	data, err := os.ReadFile(testdataPath("golden", rel))
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, filepath.ToSlash(line))
		}
	}
	sort.Strings(lines)
	return lines, nil
}

func listTarNames(archivePath string) ([]string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	tr := tar.NewReader(f)
	var names []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		names = append(names, filepath.ToSlash(hdr.Name))
	}
	sort.Strings(names)
	return names, nil
}
