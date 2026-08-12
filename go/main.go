package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

type Config struct {
	Directory    string
	Output       string
	Recursive    bool
	Verbose      bool
	Quiet        bool
	Force        bool
	Include      []string
	Exclude      []string
	Compression  int
	Format       string
	FollowLinks  bool
	MaxDepth     int
	MaxFiles     uint64
	MaxTotalSize uint64
	MaxFileSize  uint64
}

func main() {
	if err := NewRootCommand().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// NewRootCommand builds the CLI without relying on a package-level Config.
func NewRootCommand() *cobra.Command {
	cfg := &Config{
		Directory:   ".",
		Compression: 6,
		Format:      "tar.gz",
		MaxDepth:    -1,
	}

	cmd := &cobra.Command{
		Use:   "dirtofile",
		Short: "Pack directory contents into a single file",
		Long: `dirtofile is a CLI utility that packs directory contents into a single file.
It supports recursive traversal, filtering, compression, and various output formats.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPack(cfg)
		},
	}

	cmd.Flags().StringVarP(&cfg.Directory, "directory", "d", ".", "Source directory to pack")
	cmd.Flags().StringVarP(&cfg.Output, "output", "o", "", "Output file (required)")
	cmd.Flags().BoolVarP(&cfg.Recursive, "recursive", "r", false, "Recursively traverse subdirectories")
	cmd.Flags().BoolVarP(&cfg.Verbose, "verbose", "v", false, "Verbose output")
	cmd.Flags().BoolVarP(&cfg.Quiet, "quiet", "q", false, "Quiet mode (suppress all output except errors)")
	cmd.Flags().BoolVar(&cfg.Force, "force", false, "Overwrite existing output file")
	cmd.Flags().StringSliceVarP(&cfg.Include, "include", "i", nil, "Include patterns (can be specified multiple times)")
	cmd.Flags().StringSliceVarP(&cfg.Exclude, "exclude", "e", nil, "Exclude patterns (can be specified multiple times)")
	cmd.Flags().IntVar(&cfg.Compression, "compression", 6, "Compression level (0-9, 0=no compression)")
	cmd.Flags().StringVar(&cfg.Format, "format", "tar.gz", "Output format: tar.gz, tar, custom")
	cmd.Flags().BoolVar(&cfg.FollowLinks, "follow-links", false, "Follow symbolic links")
	cmd.Flags().IntVar(&cfg.MaxDepth, "max-depth", -1, "Maximum recursion depth (-1 = unlimited)")
	cmd.Flags().Uint64Var(&cfg.MaxFiles, "max-files", 0, "Maximum number of files to pack (0 = unlimited)")
	cmd.Flags().Uint64Var(&cfg.MaxTotalSize, "max-total-size", 0, "Maximum total uncompressed bytes (0 = unlimited)")
	cmd.Flags().Uint64Var(&cfg.MaxFileSize, "max-file-size", 0, "Maximum size per file in bytes (0 = unlimited)")

	_ = cmd.MarkFlagRequired("output")

	return cmd
}
