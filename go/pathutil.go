package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func resolveCanonicalRoot(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

func isUnderRoot(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return false
	}
	return true
}

func relDepth(relPath string) int {
	relPath = filepath.ToSlash(filepath.Clean(relPath))
	if relPath == "." {
		return 0
	}
	return strings.Count(relPath, "/")
}

func sanitizeArchivePath(rel string) (string, error) {
	if rel == "" {
		return "", fmt.Errorf("empty archive path")
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("absolute paths are not allowed in archives: %s", rel)
	}

	clean := filepath.ToSlash(filepath.Clean(rel))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("parent directory segments are not allowed: %s", rel)
	}

	for _, part := range strings.Split(clean, "/") {
		if part == ".." {
			return "", fmt.Errorf("parent directory segments are not allowed: %s", rel)
		}
	}
	if clean == "." {
		return "", fmt.Errorf("empty archive path after normalization")
	}

	return clean, nil
}

func validateCustomPathChars(path string) error {
	if strings.ContainsAny(path, "\n\r") {
		return fmt.Errorf("archive path contains invalid control characters: %q", path)
	}
	return nil
}

func validateCompression(level int) error {
	if level < 0 || level > 9 {
		return fmt.Errorf("compression level must be between 0 and 9, got %d", level)
	}
	return nil
}

func validateOutput(cfg *Config) error {
	parent := filepath.Dir(cfg.Output)
	if parent != "" && parent != "." {
		if _, err := os.Stat(parent); err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("output parent directory does not exist: %s", parent)
			}
			return fmt.Errorf("cannot access output parent directory: %w", err)
		}
	}

	info, err := os.Stat(cfg.Output)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("output path is a directory: %s", cfg.Output)
	}
	if !cfg.Force {
		return fmt.Errorf("output file already exists: %s (use --force to overwrite)", cfg.Output)
	}
	return nil
}

type packLimits struct {
	maxFiles     uint64
	maxTotalSize uint64
	maxFileSize  uint64
	filesPacked  uint64
	bytesPacked  uint64
}

func newPackLimits(cfg *Config) packLimits {
	return packLimits{
		maxFiles:     cfg.MaxFiles,
		maxTotalSize: cfg.MaxTotalSize,
		maxFileSize:  cfg.MaxFileSize,
	}
}

func (l *packLimits) admit(size uint64) error {
	if l.maxFileSize > 0 && size > l.maxFileSize {
		return fmt.Errorf("file exceeds --max-file-size (%d bytes): file size is %d", l.maxFileSize, size)
	}
	if l.maxFiles > 0 && l.filesPacked >= l.maxFiles {
		return fmt.Errorf("exceeded --max-files limit (%d)", l.maxFiles)
	}
	if l.maxTotalSize > 0 && l.bytesPacked+size > l.maxTotalSize {
		return fmt.Errorf("exceeded --max-total-size limit (%d bytes)", l.maxTotalSize)
	}
	l.filesPacked++
	l.bytesPacked += size
	return nil
}
