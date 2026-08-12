package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/bmatcuk/doublestar/v4"
)

type fileEntry struct {
	path        string
	archiveName string
	size        int64
}

func applyRunConfig(cfg *Config) {
	if cfg.Quiet {
		cfg.Verbose = false
	}
}

func runPack(cfg *Config) error {
	applyRunConfig(cfg)

	if err := validateCompression(cfg.Compression); err != nil {
		return err
	}

	info, err := os.Stat(cfg.Directory)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("source directory does not exist: %s", cfg.Directory)
		}
		return fmt.Errorf("cannot access source directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("source path is not a directory: %s", cfg.Directory)
	}

	if err := validateOutput(cfg); err != nil {
		return err
	}

	count, err := pack(cfg)
	if err != nil {
		return err
	}

	if !cfg.Quiet {
		fmt.Printf("Successfully created archive: %s (%d files)\n", cfg.Output, count)
	}
	return nil
}

func pack(cfg *Config) (int, error) {
	switch cfg.Format {
	case "tar.gz":
		return packTarGz(cfg)
	case "tar":
		return packTar(cfg)
	case "custom":
		return packCustom(cfg)
	default:
		return 0, fmt.Errorf("unsupported format: %s", cfg.Format)
	}
}

func packTarGz(cfg *Config) (int, error) {
	outFile, err := os.Create(cfg.Output)
	if err != nil {
		return 0, fmt.Errorf("failed to create output file: %w", err)
	}
	defer outFile.Close()

	gzipWriter, err := gzip.NewWriterLevel(outFile, cfg.Compression)
	if err != nil {
		return 0, fmt.Errorf("invalid compression level: %w", err)
	}
	defer gzipWriter.Close()

	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	count, err := walkAndPack(cfg, func(entry fileEntry) error {
		return addFileToTar(tarWriter, cfg, entry)
	})
	if err != nil {
		return 0, err
	}
	if !cfg.Quiet {
		fmt.Printf("Packed %d files\n", count)
	}
	return count, nil
}

func packTar(cfg *Config) (int, error) {
	outFile, err := os.Create(cfg.Output)
	if err != nil {
		return 0, fmt.Errorf("failed to create output file: %w", err)
	}
	defer outFile.Close()

	tarWriter := tar.NewWriter(outFile)
	defer tarWriter.Close()

	count, err := walkAndPack(cfg, func(entry fileEntry) error {
		return addFileToTar(tarWriter, cfg, entry)
	})
	if err != nil {
		return 0, err
	}
	if !cfg.Quiet {
		fmt.Printf("Packed %d files\n", count)
	}
	return count, nil
}

func packCustom(cfg *Config) (int, error) {
	limits := newPackLimits(cfg)
	count, err := walkAndCount(cfg, &limits)
	if err != nil {
		return 0, err
	}

	outFile, err := os.Create(cfg.Output)
	if err != nil {
		return 0, fmt.Errorf("failed to create output file: %w", err)
	}
	defer outFile.Close()

	header := fmt.Sprintf("DIRTOFILE_ARCHIVE_v1\n%d\n", count)
	if _, err := outFile.WriteString(header); err != nil {
		return 0, err
	}

	_, err = walkAndPack(cfg, func(entry fileEntry) error {
		return addFileToCustomArchive(outFile, cfg, entry)
	})
	if err != nil {
		return 0, err
	}
	if !cfg.Quiet {
		fmt.Printf("Packed %d files\n", count)
	}
	return count, nil
}

func walkAndCount(cfg *Config, limits *packLimits) (int, error) {
	count := 0
	err := walkEntries(cfg, func(entry fileEntry) error {
		if err := limits.admit(uint64(entry.size)); err != nil {
			return err
		}
		count++
		return nil
	})
	return count, err
}

func walkAndPack(cfg *Config, packOne func(fileEntry) error) (int, error) {
	limits := newPackLimits(cfg)
	count := 0
	err := walkEntries(cfg, func(entry fileEntry) error {
		if err := limits.admit(uint64(entry.size)); err != nil {
			return err
		}
		if err := packOne(entry); err != nil {
			return err
		}
		count++
		return nil
	})
	return count, err
}

func walkEntries(cfg *Config, onFile func(fileEntry) error) error {
	canonicalRoot, err := resolveCanonicalRoot(cfg.Directory)
	if err != nil {
		return fmt.Errorf("cannot resolve source directory: %w", err)
	}

	return filepath.WalkDir(cfg.Directory, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("cannot access %s: %w", path, walkErr)
		}

		relPath, err := filepath.Rel(cfg.Directory, path)
		if err != nil {
			return err
		}

		if cfg.MaxDepth >= 0 {
			if relDepth(relPath) > cfg.MaxDepth {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		if d.IsDir() {
			if !cfg.Recursive && path != cfg.Directory {
				return filepath.SkipDir
			}
			return nil
		}

		entryInfo, err := d.Info()
		if err != nil {
			return fmt.Errorf("cannot stat %s: %w", path, err)
		}

		readPath := path

		if entryInfo.Mode()&os.ModeSymlink != 0 {
			if !cfg.FollowLinks {
				if cfg.Verbose {
					fmt.Printf("Skipping symlink: %s\n", path)
				}
				return nil
			}

			target, err := os.Readlink(path)
			if err != nil {
				return fmt.Errorf("cannot read symlink %s: %w", path, err)
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(path), target)
			}
			readPath = target

			targetInfo, err := os.Stat(target)
			if err != nil {
				return fmt.Errorf("cannot stat symlink target %s: %w", target, err)
			}
			if targetInfo.IsDir() {
				if cfg.Verbose {
					fmt.Printf("Skipping symlink to directory: %s\n", path)
				}
				return nil
			}
			entryInfo = targetInfo
		}

		canonicalPath, err := filepath.EvalSymlinks(readPath)
		if err != nil {
			return fmt.Errorf("cannot resolve %s: %w", readPath, err)
		}

		if !isUnderRoot(canonicalPath, canonicalRoot) {
			if cfg.Verbose {
				fmt.Printf("Skipping %s: resolves outside source directory\n", path)
			}
			return nil
		}

		if !shouldIncludeFile(cfg, relPath) {
			if cfg.Verbose {
				fmt.Printf("Excluding file: %s\n", relPath)
			}
			return nil
		}

		archiveName, err := sanitizeArchivePath(relPath)
		if err != nil {
			return fmt.Errorf("unsafe archive path %s: %w", relPath, err)
		}
		if err := validateCustomPathChars(archiveName); err != nil {
			return err
		}

		entry := fileEntry{
			path:        readPath,
			archiveName: archiveName,
			size:        entryInfo.Size(),
		}

		if cfg.Verbose {
			fmt.Printf("Including file: %s (%d bytes)\n", relPath, entry.size)
		}

		return onFile(entry)
	})
}

func shouldIncludeFile(cfg *Config, relPath string) bool {
	for _, pattern := range cfg.Exclude {
		if matchGlob(pattern, relPath) {
			return false
		}
	}

	if len(cfg.Include) > 0 {
		for _, pattern := range cfg.Include {
			if matchGlob(pattern, relPath) {
				return true
			}
		}
		return false
	}

	return true
}

func matchGlob(pattern, relPath string) bool {
	relPath = filepath.ToSlash(relPath)
	pattern = filepath.ToSlash(pattern)

	if matched, err := doublestar.Match(pattern, relPath); err == nil && matched {
		return true
	}
	base := filepath.Base(relPath)
	if matched, err := doublestar.Match(pattern, base); err == nil && matched {
		return true
	}
	return false
}

func addFileToTar(tarWriter *tar.Writer, cfg *Config, entry fileEntry) error {
	file, err := os.Open(entry.path)
	if err != nil {
		return err
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return err
	}
	if stat.Size() != entry.size {
		return fmt.Errorf(
			"file size changed during archiving: %s (expected %d bytes, now %d)",
			entry.archiveName, entry.size, stat.Size(),
		)
	}

	header := &tar.Header{
		Name:    entry.archiveName,
		Size:    stat.Size(),
		Mode:    int64(stat.Mode()),
		ModTime: stat.ModTime(),
	}

	if err := tarWriter.WriteHeader(header); err != nil {
		return err
	}

	if _, err := io.Copy(tarWriter, file); err != nil {
		return err
	}

	if cfg.Verbose {
		fmt.Printf("Added to archive: %s\n", entry.archiveName)
	}

	return nil
}

func addFileToCustomArchive(writer io.Writer, cfg *Config, entry fileEntry) error {
	file, err := os.Open(entry.path)
	if err != nil {
		return err
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return err
	}
	if stat.Size() != entry.size {
		return fmt.Errorf(
			"file size changed during archiving: %s (expected %d bytes, now %d)",
			entry.archiveName, entry.size, stat.Size(),
		)
	}

	metadata := fmt.Sprintf("FILE:%s\nSIZE:%d\nMTIME:%d\n", entry.archiveName, stat.Size(), stat.ModTime().Unix())
	if _, err := io.WriteString(writer, metadata); err != nil {
		return err
	}

	if _, err := io.Copy(writer, file); err != nil {
		return err
	}

	if _, err := io.WriteString(writer, "\n---END_FILE---\n"); err != nil {
		return err
	}

	if cfg.Verbose {
		fmt.Printf("Added to custom archive: %s\n", entry.archiveName)
	}

	return nil
}
