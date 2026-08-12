package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestMaxDepthZeroSkipsSubdirectory(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "root.txt"), []byte("r"), 0o644)
	sub := filepath.Join(dir, "sub")
	_ = os.Mkdir(sub, 0o755)
	_ = os.WriteFile(filepath.Join(sub, "nested.txt"), []byte("n"), 0o644)

	out := filepath.Join(t.TempDir(), "out.tar")
	cfg := &Config{
		Directory: dir,
		Output:    out,
		Format:    "tar",
		Recursive: true,
		MaxDepth:  0,
		Force:     true,
		Quiet:     true,
	}

	count, err := pack(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("max-depth 0: want 1 file, got %d", count)
	}

	names, err := listTarNames(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "root.txt" {
		t.Fatalf("unexpected names: %v", names)
	}
}

func TestCompressionLevelsProduceDifferentSizes(t *testing.T) {
	dir := t.TempDir()
	// Highly compressible payload
	payload := bytes.Repeat([]byte("abcdefghij"), 8000)
	_ = os.WriteFile(filepath.Join(dir, "big.txt"), payload, 0o644)

	outLow := filepath.Join(t.TempDir(), "low.tar.gz")
	outHigh := filepath.Join(t.TempDir(), "high.tar.gz")

	cfgLow := &Config{
		Directory:   dir,
		Output:      outLow,
		Format:      "tar.gz",
		Recursive:   false,
		Compression: 1,
		Force:       true,
		Quiet:       true,
	}
	cfgHigh := &Config{
		Directory:   dir,
		Output:      outHigh,
		Format:      "tar.gz",
		Recursive:   false,
		Compression: 9,
		Force:       true,
		Quiet:       true,
	}

	if _, err := pack(cfgLow); err != nil {
		t.Fatalf("pack low: %v", err)
	}
	if _, err := pack(cfgHigh); err != nil {
		t.Fatalf("pack high: %v", err)
	}

	lowInfo, err := os.Stat(outLow)
	if err != nil {
		t.Fatal(err)
	}
	highInfo, err := os.Stat(outHigh)
	if err != nil {
		t.Fatal(err)
	}
	if highInfo.Size() >= lowInfo.Size() {
		t.Fatalf("expected compression 9 (%d) smaller than 1 (%d)", highInfo.Size(), lowInfo.Size())
	}
}

func TestQuietSuppressesStdout(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644)
	out := filepath.Join(t.TempDir(), "out.tar")

	stdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	done := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	cfg := &Config{
		Directory: dir,
		Output:    out,
		Format:    "tar",
		Force:     true,
		Quiet:     true,
	}
	packErr := runPack(cfg)

	w.Close()
	os.Stdout = stdout
	output := <-done

	if packErr != nil {
		t.Fatalf("runPack: %v", packErr)
	}
	if output != "" {
		t.Fatalf("quiet mode printed to stdout: %q", output)
	}
}

func TestVerboseQuietMutualExclusion(t *testing.T) {
	cfg := &Config{Quiet: true, Verbose: true}
	applyRunConfig(cfg)
	if cfg.Verbose {
		t.Fatal("verbose must be false when quiet is set")
	}
}

func TestSourceFileNotDirectory(t *testing.T) {
	f := filepath.Join(t.TempDir(), "notadir.txt")
	_ = os.WriteFile(f, []byte("x"), 0o644)

	cfg := &Config{
		Directory: f,
		Output:    filepath.Join(t.TempDir(), "out.tar"),
	}
	if err := runPack(cfg); err == nil {
		t.Fatal("expected error when -d is a file")
	}
}

func TestIncludeExcludeGlob(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("k"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "drop.log"), []byte("d"), 0o644)
	sub := filepath.Join(dir, "src")
	_ = os.Mkdir(sub, 0o755)
	_ = os.WriteFile(filepath.Join(sub, "main.rs"), []byte("rs"), 0o644)

	out := filepath.Join(t.TempDir(), "out.tar")
	cfg := &Config{
		Directory: dir,
		Output:    out,
		Format:    "tar",
		Recursive: true,
		Exclude:   []string{"*.log"},
		Include:   []string{"**/*.rs", "*.txt"},
		Force:     true,
		Quiet:     true,
	}

	count, err := pack(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("want 2 files, got %d", count)
	}

	names, _ := listTarNames(out)
	for _, n := range names {
		if filepath.Ext(n) == ".log" {
			t.Fatalf("exclude failed: %v", names)
		}
	}
}
