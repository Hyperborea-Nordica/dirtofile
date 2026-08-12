use std::fs::File;
use std::io::{BufReader, BufWriter, Read, Write};
use std::path::{Component, Path, PathBuf};
use std::time::SystemTime;

use anyhow::{Context, Result, bail};
use clap::{Parser, ValueEnum};
use flate2::write::GzEncoder;
use flate2::Compression;
use globset::{Glob, GlobSet, GlobSetBuilder};
use tar::Builder;
use walkdir::WalkDir;

const COPY_BUFFER_SIZE: usize = 64 * 1024;

#[derive(Clone, Parser)]
#[command(name = "dirtofile")]
#[command(about = "Pack directory contents into a single file")]
#[command(long_about = "dirtofile is a CLI utility that packs directory contents into a single file.
It supports recursive traversal, filtering, compression, and various output formats.")]
struct Args {
    /// Source directory to pack
    #[arg(short = 'd', long, default_value = ".")]
    directory: PathBuf,

    /// Output file
    #[arg(short = 'o', long)]
    output: PathBuf,

    /// Recursively traverse subdirectories
    #[arg(short = 'r', long)]
    recursive: bool,

    /// Verbose output
    #[arg(short = 'v', long)]
    verbose: bool,

    /// Quiet mode (suppress all output except errors)
    #[arg(short = 'q', long)]
    quiet: bool,

    /// Overwrite existing output file
    #[arg(long)]
    force: bool,

    /// Include patterns (can be specified multiple times)
    #[arg(short = 'i', long)]
    include: Vec<String>,

    /// Exclude patterns (can be specified multiple times)
    #[arg(short = 'e', long)]
    exclude: Vec<String>,

    /// Compression level (0-9, 0=no compression)
    #[arg(long, default_value = "6")]
    compression: u32,

    /// Output format
    #[arg(long, default_value = "tar.gz")]
    format: ArchiveFormat,

    /// Follow symbolic links
    #[arg(long)]
    follow_links: bool,

    /// Maximum recursion depth (-1 = unlimited)
    #[arg(long, default_value = "-1")]
    max_depth: isize,

    /// Maximum number of files to pack (0 = unlimited)
    #[arg(long, default_value = "0")]
    max_files: u64,

    /// Maximum total uncompressed bytes (0 = unlimited)
    #[arg(long, default_value = "0")]
    max_total_size: u64,

    /// Maximum size per file in bytes (0 = unlimited)
    #[arg(long, default_value = "0")]
    max_file_size: u64,
}

#[derive(Clone, ValueEnum)]
enum ArchiveFormat {
    #[clap(name = "tar.gz")]
    TarGz,
    #[clap(name = "tar")]
    Tar,
    Custom,
}

struct FileInfo {
    path: PathBuf,
    archive_name: String,
    size: u64,
    modified: SystemTime,
}

struct PatternFilters {
    include: GlobSet,
    exclude: GlobSet,
    require_include: bool,
}

impl PatternFilters {
    fn from_args(args: &Args) -> Result<Self> {
        Ok(Self {
            include: compile_glob_set(&args.include)?,
            exclude: compile_glob_set(&args.exclude)?,
            require_include: !args.include.is_empty(),
        })
    }

    fn matches(&self, relative_path: &Path) -> bool {
        if self.exclude.is_match_path(relative_path) {
            return false;
        }
        if self.require_include {
            return self.include.is_match_path(relative_path)
                || relative_path
                    .file_name()
                    .is_some_and(|name| self.include.is_match_path(name));
        }
        true
    }
}

struct PackLimits {
    max_files: Option<u64>,
    max_total_size: Option<u64>,
    max_file_size: Option<u64>,
    files_packed: u64,
    bytes_packed: u64,
}

impl PackLimits {
    fn from_args(args: &Args) -> Self {
        Self {
            max_files: (args.max_files > 0).then_some(args.max_files),
            max_total_size: (args.max_total_size > 0).then_some(args.max_total_size),
            max_file_size: (args.max_file_size > 0).then_some(args.max_file_size),
            files_packed: 0,
            bytes_packed: 0,
        }
    }

    fn admit(&mut self, size: u64) -> Result<()> {
        if let Some(max) = self.max_file_size {
            if size > max {
                bail!("File exceeds --max-file-size ({max} bytes): file size is {size}");
            }
        }
        if let Some(max) = self.max_files {
            if self.files_packed >= max {
                bail!("Exceeded --max-files limit ({max})");
            }
        }
        if let Some(max) = self.max_total_size {
            if self.bytes_packed.saturating_add(size) > max {
                bail!("Exceeded --max-total-size limit ({max} bytes)");
            }
        }
        self.files_packed += 1;
        self.bytes_packed += size;
        Ok(())
    }
}

fn main() -> Result<()> {
    let mut args = Args::parse();
    run(&mut args)
}

fn apply_run_config(args: &mut Args) {
    if args.quiet {
        args.verbose = false;
    }
}

fn run(args: &mut Args) -> Result<()> {
    apply_run_config(args);

    if args.compression > 9 {
        bail!(
            "Compression level must be between 0 and 9, got {}",
            args.compression
        );
    }

    let dir_meta = std::fs::metadata(&args.directory).with_context(|| {
        format!(
            "Cannot access source directory: {}",
            args.directory.display()
        )
    })?;
    if !dir_meta.is_dir() {
        bail!(
            "Source path is not a directory: {}",
            args.directory.display()
        );
    }

    validate_output(args)?;

    let filters = PatternFilters::from_args(args)?;
    let canonical_root = args.directory.canonicalize().with_context(|| {
        format!(
            "Cannot resolve source directory: {}",
            args.directory.display()
        )
    })?;

    let file_count = match args.format {
        ArchiveFormat::TarGz => pack_tar_gz(args, &filters, &canonical_root)?,
        ArchiveFormat::Tar => pack_tar(args, &filters, &canonical_root)?,
        ArchiveFormat::Custom => pack_custom(args, &filters, &canonical_root)?,
    };

    if !args.quiet {
        println!("Successfully created archive: {} ({file_count} files)", args.output.display());
    }

    Ok(())
}

fn validate_output(args: &Args) -> Result<()> {
    if let Some(parent) = args.output.parent() {
        if !parent.as_os_str().is_empty() && !parent.exists() {
            bail!(
                "Output parent directory does not exist: {}",
                parent.display()
            );
        }
    }

    if args.output.exists() {
        let meta = std::fs::metadata(&args.output)?;
        if meta.is_dir() {
            bail!("Output path is a directory: {}", args.output.display());
        }
        if !args.force {
            bail!(
                "Output file already exists: {} (use --force to overwrite)",
                args.output.display()
            );
        }
    }

    Ok(())
}

fn compile_glob_set(patterns: &[String]) -> Result<GlobSet> {
    let mut builder = GlobSetBuilder::new();
    for pattern in patterns {
        let glob = Glob::new(pattern)
            .or_else(|_| Glob::new(&pattern.replace('\\', "/")))
            .with_context(|| format!("Invalid include/exclude pattern: {pattern}"))?;
        builder.add(glob);
    }
    builder
        .build()
        .context("Failed to build glob matcher set")
}

fn pack_tar_gz(args: &Args, filters: &PatternFilters, canonical_root: &Path) -> Result<u64> {
    let output = File::create(&args.output).with_context(|| {
        format!(
            "Failed to create output file: {}",
            args.output.display()
        )
    })?;
    let encoder = GzEncoder::new(output, Compression::new(args.compression));
    let mut tar_builder = Builder::new(encoder);
    let count = walk_and_pack(args, filters, canonical_root, &mut PackLimits::from_args(args), |fi| {
        add_file_to_tar(&mut tar_builder, fi)
    })?;
    tar_builder.finish()?;
    if !args.quiet {
        println!("Packed {count} files");
    }
    Ok(count)
}

fn pack_tar(args: &Args, filters: &PatternFilters, canonical_root: &Path) -> Result<u64> {
    let output = File::create(&args.output).with_context(|| {
        format!(
            "Failed to create output file: {}",
            args.output.display()
        )
    })?;
    let mut tar_builder = Builder::new(output);
    let count = walk_and_pack(args, filters, canonical_root, &mut PackLimits::from_args(args), |fi| {
        add_file_to_tar(&mut tar_builder, fi)
    })?;
    tar_builder.finish()?;
    if !args.quiet {
        println!("Packed {count} files");
    }
    Ok(count)
}

fn pack_custom(args: &Args, filters: &PatternFilters, canonical_root: &Path) -> Result<u64> {
    let mut limits = PackLimits::from_args(args);
    let count = walk_and_count(args, filters, canonical_root, &mut limits)?;

    let output = File::create(&args.output).with_context(|| {
        format!(
            "Failed to create output file: {}",
            args.output.display()
        )
    })?;
    let mut writer = BufWriter::new(output);
    writeln!(writer, "DIRTOFILE_ARCHIVE_v1")?;
    writeln!(writer, "{count}")?;

    let mut limits = PackLimits::from_args(args);
    walk_and_pack(args, filters, canonical_root, &mut limits, |fi| {
        add_file_to_custom_archive(&mut writer, fi)
    })?;

    writer.flush()?;
    if !args.quiet {
        println!("Packed {count} files");
    }
    Ok(count)
}

fn walk_and_count(
    args: &Args,
    filters: &PatternFilters,
    canonical_root: &Path,
    limits: &mut PackLimits,
) -> Result<u64> {
    let mut count = 0u64;
    walk_entries(args, filters, canonical_root, |fi| {
        limits.admit(fi.size)?;
        count += 1;
        Ok(())
    })?;
    Ok(count)
}

fn walk_and_pack<F>(
    args: &Args,
    filters: &PatternFilters,
    canonical_root: &Path,
    limits: &mut PackLimits,
    mut pack_one: F,
) -> Result<u64>
where
    F: FnMut(&FileInfo) -> Result<()>,
{
    let mut count = 0u64;
    walk_entries(args, filters, canonical_root, |fi| {
        limits.admit(fi.size)?;
        pack_one(fi)?;
        count += 1;
        Ok(())
    })?;
    Ok(count)
}

fn walk_entries<F>(
    args: &Args,
    filters: &PatternFilters,
    canonical_root: &Path,
    mut on_file: F,
) -> Result<()>
where
    F: FnMut(&FileInfo) -> Result<()>,
{
    let base_path = &args.directory;
    let walker = WalkDir::new(base_path)
        .follow_links(args.follow_links)
        .max_depth(if args.max_depth >= 0 {
            args.max_depth as usize
        } else {
            usize::MAX
        });

    for entry in walker {
        let entry = entry?;
        let path = entry.path();

        if path.is_dir() {
            if !args.recursive && path != base_path.as_path() {
                entry.skip_current_dir();
            }
            continue;
        }

        if let Ok(link_meta) = std::fs::symlink_metadata(path) {
            if link_meta.file_type().is_symlink() && !args.follow_links {
                if args.verbose {
                    eprintln!("Skipping symlink: {}", path.display());
                }
                continue;
            }
        }

        let canonical_path = match path.canonicalize() {
            Ok(p) => p,
            Err(e) => {
                if args.verbose {
                    eprintln!("Skipping {}: cannot resolve path: {e}", path.display());
                }
                continue;
            }
        };

        if !canonical_path.starts_with(canonical_root) {
            if args.verbose {
                eprintln!(
                    "Skipping {}: resolves outside source directory",
                    path.display()
                );
            }
            continue;
        }

        let relative_path = path
            .strip_prefix(base_path)
            .context("Failed to get relative path")?;

        if !filters.matches(relative_path) {
            if args.verbose {
                println!("Excluding file: {}", relative_path.display());
            }
            continue;
        }

        let archive_name = sanitize_archive_path(relative_path)
            .with_context(|| format!("Unsafe archive path: {}", relative_path.display()))?;
        validate_custom_path_chars(&archive_name)?;

        let metadata = entry.metadata()?;
        let file_info = FileInfo {
            path: path.to_path_buf(),
            archive_name,
            size: metadata.len(),
            modified: metadata.modified()?,
        };

        if args.verbose {
            println!(
                "Including file: {} ({} bytes)",
                relative_path.display(),
                file_info.size
            );
        }

        on_file(&file_info)?;
    }

    Ok(())
}

fn add_file_to_tar<W: Write>(tar_builder: &mut Builder<W>, file_info: &FileInfo) -> Result<()> {
    let current = std::fs::metadata(&file_info.path).with_context(|| {
        format!(
            "Failed to stat file before archiving: {}",
            file_info.path.display()
        )
    })?;
    if current.len() != file_info.size {
        bail!(
            "File size changed during archiving: {} (expected {} bytes, now {})",
            file_info.archive_name,
            file_info.size,
            current.len()
        );
    }

    tar_builder
        .append_path_with_name(&file_info.path, &file_info.archive_name)
        .with_context(|| format!("Failed to add file to archive: {}", file_info.archive_name))?;

    Ok(())
}

fn add_file_to_custom_archive(
    writer: &mut BufWriter<File>,
    file_info: &FileInfo,
) -> Result<()> {
    let mut file = File::open(&file_info.path)
        .with_context(|| format!("Failed to open file: {}", file_info.path.display()))?;

    let current = file.metadata()?;
    if current.len() != file_info.size {
        bail!(
            "File size changed during archiving: {} (expected {} bytes, now {})",
            file_info.archive_name,
            file_info.size,
            current.len()
        );
    }

    let modified_timestamp = file_info
        .modified
        .duration_since(std::time::UNIX_EPOCH)?
        .as_secs();

    writeln!(writer, "FILE:{}", file_info.archive_name)?;
    writeln!(writer, "SIZE:{}", file_info.size)?;
    writeln!(writer, "MTIME:{}", modified_timestamp)?;

    let mut reader = BufReader::new(&mut file);
    let mut buffer = [0u8; COPY_BUFFER_SIZE];
    loop {
        let n = reader.read(&mut buffer)?;
        if n == 0 {
            break;
        }
        writer.write_all(&buffer[..n])?;
    }

    writeln!(writer)?;
    writeln!(writer, "---END_FILE---")?;

    Ok(())
}

fn sanitize_archive_path(rel: &Path) -> Result<String> {
    if rel.as_os_str().is_empty() {
        bail!("Empty archive path");
    }
    if rel.is_absolute() {
        bail!("Absolute paths are not allowed in archives: {}", rel.display());
    }

    let mut parts = Vec::new();
    for component in rel.components() {
        match component {
            Component::Normal(name) => parts.push(name.to_string_lossy().into_owned()),
            Component::CurDir => {}
            Component::ParentDir => {
                bail!("Parent directory segments are not allowed: {}", rel.display());
            }
            Component::RootDir | Component::Prefix(_) => {
                bail!("Absolute path components are not allowed: {}", rel.display());
            }
        }
    }

    if parts.is_empty() {
        bail!("Empty archive path after normalization");
    }

    Ok(parts.join("/"))
}

fn validate_custom_path_chars(path: &str) -> Result<()> {
    if path.contains('\n') || path.contains('\r') {
        bail!("Archive path contains invalid control characters: {path:?}");
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use proptest::prelude::*;
    use std::fs;
    use tempfile::tempdir;

    #[test]
    fn sanitize_rejects_parent_dir() {
        assert!(sanitize_archive_path(Path::new("../secret")).is_err());
        assert!(sanitize_archive_path(Path::new("foo/../../bar")).is_err());
    }

    #[test]
    fn sanitize_normalizes_slashes() {
        let name = sanitize_archive_path(Path::new("a/b/c.txt")).unwrap();
        assert_eq!(name, "a/b/c.txt");
    }

    #[test]
    fn validate_custom_path_rejects_newlines() {
        assert!(validate_custom_path_chars("ok.txt").is_ok());
        assert!(validate_custom_path_chars("bad\nname").is_err());
    }

    #[test]
    fn globset_supports_double_star() {
        let filters = PatternFilters {
            include: compile_glob_set(&["src/**/*.rs".to_string()]).unwrap(),
            exclude: compile_glob_set(&[]).unwrap(),
            require_include: true,
        };
        assert!(filters.matches(Path::new("src/foo/bar.rs")));
        assert!(!filters.matches(Path::new("other/bar.rs")));
    }

    #[test]
    fn non_recursive_packs_only_root_files() -> Result<()> {
        let tmp = tempdir()?;
        fs::write(tmp.path().join("root.txt"), b"root")?;
        let sub = tmp.path().join("sub");
        fs::create_dir(&sub)?;
        fs::write(sub.join("nested.txt"), b"nested")?;

        let out = tmp.path().join("out.tar");
        let mut args = Args {
            directory: tmp.path().to_path_buf(),
            output: out.clone(),
            recursive: false,
            verbose: false,
            quiet: true,
            force: true,
            include: vec![],
            exclude: vec![],
            compression: 6,
            format: ArchiveFormat::Tar,
            follow_links: false,
            max_depth: -1,
            max_files: 0,
            max_total_size: 0,
            max_file_size: 0,
        };

        let count = pack_tar(&args, &PatternFilters::from_args(&args)?, &args.directory.canonicalize()?)?;
        assert_eq!(count, 1);

        let file = File::open(out)?;
        let mut archive = tar::Archive::new(file);
        let mut names = Vec::new();
        for entry in archive.entries()? {
            names.push(entry?.path()?.to_string_lossy().into_owned());
        }
        assert_eq!(names.len(), 1);
        assert_eq!(names[0], "root.txt");
        Ok(())
    }

    #[test]
    fn validate_output_rejects_existing_without_force() -> Result<()> {
        let tmp = tempdir()?;
        let existing = tmp.path().join("exists.tar");
        fs::write(&existing, b"x")?;

        let args = Args {
            directory: tmp.path().to_path_buf(),
            output: existing,
            recursive: false,
            verbose: false,
            quiet: true,
            force: false,
            include: vec![],
            exclude: vec![],
            compression: 6,
            format: ArchiveFormat::Tar,
            follow_links: false,
            max_depth: -1,
            max_files: 0,
            max_total_size: 0,
            max_file_size: 0,
        };

        assert!(validate_output(&args).is_err());
        Ok(())
    }

    fn testdata_path(rel: &str) -> PathBuf {
        PathBuf::from(env!("CARGO_MANIFEST_DIR"))
            .join("..")
            .join("testdata")
            .join(rel)
    }

    fn load_golden_lines(name: &str) -> Result<Vec<String>> {
        let path = testdata_path(&format!("golden/{name}"));
        let content = std::fs::read_to_string(&path)
            .with_context(|| format!("read golden file {}", path.display()))?;
        let mut lines: Vec<String> = content
            .lines()
            .map(str::trim)
            .filter(|l| !l.is_empty())
            .map(str::to_string)
            .collect();
        lines.sort();
        Ok(lines)
    }

    fn list_tar_names(archive: &Path) -> Result<Vec<String>> {
        let file = File::open(archive)?;
        let mut archive = tar::Archive::new(file);
        let mut names = Vec::new();
        for entry in archive.entries()? {
            names.push(entry?.path()?.to_string_lossy().into_owned());
        }
        names.sort();
        Ok(names)
    }

    #[test]
    fn golden_recursive_tar_listing() -> Result<()> {
        let fixture = testdata_path("fixtures/standard");
        let expected = load_golden_lines("standard-recursive.txt")?;

        let out = tempdir()?.path().join("out.tar");
        let args = Args {
            directory: fixture,
            output: out.clone(),
            recursive: true,
            verbose: false,
            quiet: true,
            force: true,
            include: vec![],
            exclude: vec![],
            compression: 6,
            format: ArchiveFormat::Tar,
            follow_links: false,
            max_depth: -1,
            max_files: 0,
            max_total_size: 0,
            max_file_size: 0,
        };

        let filters = PatternFilters::from_args(&args)?;
        let root = args.directory.canonicalize()?;
        let count = pack_tar(&args, &filters, &root)?;
        assert_eq!(count, expected.len() as u64);

        let got = list_tar_names(&out)?;
        assert_eq!(got, expected);
        Ok(())
    }

    #[test]
    fn golden_flat_tar_listing() -> Result<()> {
        let fixture = testdata_path("fixtures/standard");
        let expected = load_golden_lines("standard-flat.txt")?;

        let out = tempdir()?.path().join("out.tar");
        let args = Args {
            directory: fixture,
            output: out.clone(),
            recursive: false,
            verbose: false,
            quiet: true,
            force: true,
            include: vec![],
            exclude: vec![],
            compression: 6,
            format: ArchiveFormat::Tar,
            follow_links: false,
            max_depth: -1,
            max_files: 0,
            max_total_size: 0,
            max_file_size: 0,
        };

        let filters = PatternFilters::from_args(&args)?;
        let root = args.directory.canonicalize()?;
        pack_tar(&args, &filters, &root)?;

        let got = list_tar_names(&out)?;
        assert_eq!(got, expected);
        Ok(())
    }

    #[cfg(unix)]
    #[test]
    fn symlink_outside_root_not_packed() -> Result<()> {
        use std::os::unix::fs::symlink;

        let tmp = tempdir()?;
        let outside = tempdir()?;
        fs::write(outside.path().join("secret.txt"), b"secret")?;
        fs::write(tmp.path().join("safe.txt"), b"safe")?;
        symlink(
            outside.path().join("secret.txt"),
            tmp.path().join("evil-link"),
        )?;

        let out = tmp.path().join("out.tar");
        let args = Args {
            directory: tmp.path().to_path_buf(),
            output: out.clone(),
            recursive: true,
            verbose: false,
            quiet: true,
            force: true,
            include: vec![],
            exclude: vec![],
            compression: 6,
            format: ArchiveFormat::Tar,
            follow_links: false,
            max_depth: -1,
            max_files: 0,
            max_total_size: 0,
            max_file_size: 0,
        };

        let filters = PatternFilters::from_args(&args)?;
        let root = args.directory.canonicalize()?;
        let count = pack_tar(&args, &filters, &root)?;
        assert_eq!(count, 1);

        let names = list_tar_names(&out)?;
        assert_eq!(names, vec!["safe.txt".to_string()]);
        Ok(())
    }

    #[cfg(unix)]
    #[test]
    fn symlink_outside_root_skipped_with_follow_links() -> Result<()> {
        use std::os::unix::fs::symlink;

        let tmp = tempdir()?;
        let outside = tempdir()?;
        fs::write(outside.path().join("secret.txt"), b"secret")?;
        fs::write(tmp.path().join("safe.txt"), b"safe")?;
        symlink(
            outside.path().join("secret.txt"),
            tmp.path().join("evil-link"),
        )?;

        let out = tmp.path().join("out.tar");
        let args = Args {
            directory: tmp.path().to_path_buf(),
            output: out.clone(),
            recursive: true,
            verbose: false,
            quiet: true,
            force: true,
            include: vec![],
            exclude: vec![],
            compression: 6,
            format: ArchiveFormat::Tar,
            follow_links: true,
            max_depth: -1,
            max_files: 0,
            max_total_size: 0,
            max_file_size: 0,
        };

        let filters = PatternFilters::from_args(&args)?;
        let root = args.directory.canonicalize()?;
        let count = pack_tar(&args, &filters, &root)?;
        assert_eq!(count, 1);
        assert_eq!(list_tar_names(&out)?, vec!["safe.txt".to_string()]);
        Ok(())
    }

    #[test]
    fn max_depth_zero_skips_subdirectories() -> Result<()> {
        let tmp = tempdir()?;
        fs::write(tmp.path().join("root.txt"), b"r")?;
        let sub = tmp.path().join("sub");
        fs::create_dir(&sub)?;
        fs::write(sub.join("nested.txt"), b"n")?;

        let out = tmp.path().join("out.tar");
        let args = Args {
            directory: tmp.path().to_path_buf(),
            output: out.clone(),
            recursive: true,
            verbose: false,
            quiet: true,
            force: true,
            include: vec![],
            exclude: vec![],
            compression: 6,
            format: ArchiveFormat::Tar,
            follow_links: false,
            max_depth: 0,
            max_files: 0,
            max_total_size: 0,
            max_file_size: 0,
        };

        let filters = PatternFilters::from_args(&args)?;
        let root = args.directory.canonicalize()?;
        let count = pack_tar(&args, &filters, &root)?;
        assert_eq!(count, 1);
        assert_eq!(list_tar_names(&out)?, vec!["root.txt".to_string()]);
        Ok(())
    }

    #[test]
    fn compression_levels_produce_different_sizes() -> Result<()> {
        let tmp = tempdir()?;
        let payload = vec![b'a'; 80_000];
        fs::write(tmp.path().join("big.txt"), &payload)?;

        let out_low = tmp.path().join("low.tar.gz");
        let out_high = tmp.path().join("high.tar.gz");

        let base = Args {
            directory: tmp.path().to_path_buf(),
            recursive: false,
            verbose: false,
            quiet: true,
            force: true,
            include: vec![],
            exclude: vec![],
            format: ArchiveFormat::TarGz,
            follow_links: false,
            max_depth: -1,
            max_files: 0,
            max_total_size: 0,
            max_file_size: 0,
        };

        let mut low = base.clone();
        low.output = out_low.clone();
        low.compression = 1;
        let filters = PatternFilters::from_args(&low)?;
        let root = low.directory.canonicalize()?;
        pack_tar_gz(&low, &filters, &root)?;

        let mut high = base;
        high.output = out_high.clone();
        high.compression = 9;
        let filters = PatternFilters::from_args(&high)?;
        pack_tar_gz(&high, &filters, &root)?;

        let low_size = fs::metadata(out_low)?.len();
        let high_size = fs::metadata(out_high)?.len();
        assert!(high_size < low_size, "level 9 ({high_size}) should be smaller than 1 ({low_size})");
        Ok(())
    }

    #[test]
    fn quiet_disables_verbose() {
        let mut args = Args {
            directory: PathBuf::from("."),
            output: PathBuf::from("out.tar"),
            recursive: false,
            verbose: true,
            quiet: true,
            force: true,
            include: vec![],
            exclude: vec![],
            compression: 6,
            format: ArchiveFormat::Tar,
            follow_links: false,
            max_depth: -1,
            max_files: 0,
            max_total_size: 0,
            max_file_size: 0,
        };
        apply_run_config(&mut args);
        assert!(!args.verbose);
    }

    #[test]
    fn source_not_directory() -> Result<()> {
        let tmp = tempdir()?;
        let file_path = tmp.path().join("notadir.txt");
        fs::write(&file_path, b"x")?;

        let mut args = Args {
            directory: file_path,
            output: tmp.path().join("out.tar"),
            recursive: false,
            verbose: false,
            quiet: true,
            force: true,
            include: vec![],
            exclude: vec![],
            compression: 6,
            format: ArchiveFormat::Tar,
            follow_links: false,
            max_depth: -1,
            max_files: 0,
            max_total_size: 0,
            max_file_size: 0,
        };

        assert!(run(&mut args).is_err());
        Ok(())
    }

    #[test]
    fn include_exclude_filters() -> Result<()> {
        let tmp = tempdir()?;
        fs::write(tmp.path().join("keep.txt"), b"k")?;
        fs::write(tmp.path().join("drop.log"), b"d")?;
        let src = tmp.path().join("src");
        fs::create_dir(&src)?;
        fs::write(src.join("main.rs"), b"rs")?;

        let out = tmp.path().join("out.tar");
        let args = Args {
            directory: tmp.path().to_path_buf(),
            output: out.clone(),
            recursive: true,
            verbose: false,
            quiet: true,
            force: true,
            include: vec!["**/*.rs".into(), "*.txt".into()],
            exclude: vec!["*.log".into()],
            compression: 6,
            format: ArchiveFormat::Tar,
            follow_links: false,
            max_depth: -1,
            max_files: 0,
            max_total_size: 0,
            max_file_size: 0,
        };

        let filters = PatternFilters::from_args(&args)?;
        let root = args.directory.canonicalize()?;
        let count = pack_tar(&args, &filters, &root)?;
        assert_eq!(count, 2);
        let names = list_tar_names(&out)?;
        assert!(names.iter().any(|n| n.ends_with("keep.txt")));
        assert!(names.iter().any(|n| n.ends_with("main.rs")));
        assert!(!names.iter().any(|n| n.ends_with(".log")));
        Ok(())
    }

    #[cfg(unix)]
    #[test]
    fn unreadable_file_fails_walk() -> Result<()> {
        use std::os::unix::fs::PermissionsExt;

        let tmp = tempdir()?;
        let secret = tmp.path().join("secret.txt");
        fs::write(&secret, b"secret")?;
        fs::set_permissions(&secret, fs::Permissions::from_mode(0o000))?;
        fs::write(tmp.path().join("ok.txt"), b"ok")?;

        let out = tmp.path().join("out.tar");
        let args = Args {
            directory: tmp.path().to_path_buf(),
            output: out,
            recursive: false,
            verbose: false,
            quiet: true,
            force: true,
            include: vec![],
            exclude: vec![],
            compression: 6,
            format: ArchiveFormat::Tar,
            follow_links: false,
            max_depth: -1,
            max_files: 0,
            max_total_size: 0,
            max_file_size: 0,
        };

        let filters = PatternFilters::from_args(&args)?;
        let root = args.directory.canonicalize()?;
        assert!(pack_tar(&args, &filters, &root).is_err());
        Ok(())
    }

    proptest! {
        #[test]
        fn sanitize_path_idempotent(rel in r"([a-zA-Z0-9._-]+)(/[a-zA-Z0-9._-]+)*") {
            let path = Path::new(&rel);
            if let Ok(sanitized) = sanitize_archive_path(path) {
                prop_assert!(!sanitized.contains(".."));
                let again = sanitize_archive_path(Path::new(&sanitized)).unwrap();
                prop_assert_eq!(sanitized, again);
            }
        }
    }

    #[test]
    fn max_files_limit() -> Result<()> {
        let tmp = tempdir()?;
        fs::write(tmp.path().join("a.txt"), b"a")?;
        fs::write(tmp.path().join("b.txt"), b"b")?;

        let out = tmp.path().join("out.tar");
        let mut args = Args {
            directory: tmp.path().to_path_buf(),
            output: out,
            recursive: false,
            verbose: false,
            quiet: true,
            force: true,
            include: vec![],
            exclude: vec![],
            compression: 6,
            format: ArchiveFormat::Tar,
            follow_links: false,
            max_depth: -1,
            max_files: 1,
            max_total_size: 0,
            max_file_size: 0,
        };

        let filters = PatternFilters::from_args(&args)?;
        let root = args.directory.canonicalize()?;
        assert!(pack_tar(&args, &filters, &root).is_err());
        Ok(())
    }
}
