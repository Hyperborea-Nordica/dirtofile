#!/usr/bin/env bash
# Cross-check tar listings: Go binary vs Rust binary vs golden file.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
FIXTURE="$ROOT/testdata/fixtures/standard"
GOLDEN="$ROOT/testdata/golden/standard-recursive.txt"
OUT_GO="$(mktemp)"
OUT_RS="$(mktemp)"
trap 'rm -f "$OUT_GO" "$OUT_RS"' EXIT

mkdir -p "$ROOT/bin"

echo "==> Building Go"
(cd "$ROOT/go" && go build -o "$ROOT/bin/dirtofile-go" .)

echo "==> Building Rust"
(cd "$ROOT/rust" && cargo build -q)
RS_BIN="$ROOT/rust/target/debug/dirtofile"

echo "==> Packing with Go"
"$ROOT/bin/dirtofile-go" -d "$FIXTURE" -o "$OUT_GO" --format tar -r -q --force

echo "==> Packing with Rust"
"$RS_BIN" -d "$FIXTURE" -o "$OUT_RS" --format tar -r -q --force

list_tar() {
  tar -tf "$1" | LC_ALL=C sort
}

GOLDEN_SORTED="$(LC_ALL=C sort "$GOLDEN")"
GO_LIST="$(list_tar "$OUT_GO")"
RS_LIST="$(list_tar "$OUT_RS")"

echo "==> Compare Go vs golden"
diff -u <(echo "$GOLDEN_SORTED") <(echo "$GO_LIST")

echo "==> Compare Rust vs golden"
diff -u <(echo "$GOLDEN_SORTED") <(echo "$RS_LIST")

echo "==> Compare Go vs Rust"
diff -u <(echo "$GO_LIST") <(echo "$RS_LIST")

echo "Golden cross-check passed."
