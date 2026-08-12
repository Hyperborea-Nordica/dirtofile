# Changelog

## [Unreleased]

### Security

- Защита от tar slip: нормализация и отклонение путей с `..`.
- Симлинки: упаковка только внутри канонического корня `-d`.
- Custom-формат: отклонение путей с `\n` / `\r`.

### Fixed

- **Rust:** флаг `-r` теперь корректно ограничивает обход (`skip_current_dir`).
- **Go:** `--compression` применяется к gzip; ошибки `WalkDir` не глотаются.
- **Rust:** `--quiet` отключает `--verbose`.
- Валидация: `-d` должен быть каталогом; сжатие 0–9.

### Added

- Golden-тесты: `testdata/`, `scripts/golden_compare.sh`, CI job `golden`.
- Symlink-тесты (Unix) для Go и Rust.
- Dependabot: Go, Cargo, GitHub Actions.
- CI matrix: Ubuntu + Windows; Go fuzz для `sanitizeArchivePath`.
- Тесты: max-depth, compression, quiet, permissions (Unix), `slow` large file.
- Rust proptest для идемпотентности `sanitize_archive_path`.
- `NewRootCommand()` в Go (без глобального `config`).

- `--force` для перезаписи выходного файла.
- `--max-files`, `--max-total-size`, `--max-file-size`.
- Потоковая упаковка tar/tar.gz (Rust); custom — двухпроходный режим.
- Проверка размера файла при записи (TOCTOU).
- `SPEC.md`, интеграционные тесты, CI workflow.
- **Go:** `doublestar` для glob `**`; `filepath.WalkDir`.

### Changed

- **Rust:** фильтры на `globset` вместо regex; удалена зависимость `chrono`.
- **Go:** логика упаковки в `pack.go`; `Config` передаётся явно в `runPack`.

### Removed

- Неиспользуемая зависимость `chrono` (Rust).
