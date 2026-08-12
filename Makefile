# Makefile for dirtofile project

.PHONY: all go rust clean test test-unit test-golden test-go test-rust help bin

# Default target
all: go rust

bin:
	mkdir -p bin

# Build Go version (all packages in go/)
go: bin
	@echo "Building Go version..."
	cd go && go mod tidy && go build -o ../bin/dirtofile-go .
	@echo "Go version built: bin/dirtofile-go"

# Build Rust version (debug)
rust:
	@echo "Building Rust version (debug)..."
	cd rust && cargo build
	@echo "Rust version built: rust/target/debug/dirtofile"

# Build Rust version (release)
rust-release:
	@echo "Building Rust version (release)..."
	cd rust && cargo build --release
	@echo "Rust version built: rust/target/release/dirtofile"

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	$(RM) bin/
	cd rust && cargo clean
	@echo "Clean complete"

# Unit tests (Go + Rust)
test-unit:
	@echo "Running Go unit tests..."
	cd go && go test -v ./...
	@echo "Running Rust unit tests..."
	cd rust && cargo test --verbose

# Golden: Go vs Rust vs testdata/golden
test-golden: go rust
	@echo "Running golden cross-check..."
	bash scripts/golden_compare.sh

# All automated tests
test: test-unit

# Slow / large-file tests (build tag: slow)
test-slow:
	@echo "Running slow tests (Go)..."
	cd go && go test -tags slow -v ./...

# Legacy manual smoke tests
test-go: go
	@echo "Smoke test Go..."
	./bin/dirtofile-go -d testdata/fixtures/standard -o test_output.tar -r -q --force
	rm -f test_output.tar
	@echo "Go smoke test OK"

test-rust: rust
	@echo "Smoke test Rust..."
	./rust/target/debug/dirtofile -d testdata/fixtures/standard -o test_output.tar -r -q --force
	rm -f test_output.tar
	@echo "Rust smoke test OK"

help:
	@echo "Available targets:"
	@echo "  all          - Build both Go and Rust versions"
	@echo "  go           - Build Go version"
	@echo "  rust         - Build Rust version (debug)"
	@echo "  rust-release - Build Rust version (release)"
	@echo "  test-unit    - go test + cargo test"
	@echo "  test-golden  - Compare Go/Rust tar listings to golden"
	@echo "  test         - Alias for test-unit"
	@echo "  test-slow    - Large-file tests (go -tags slow)"
	@echo "  clean        - Clean build artifacts"
	@echo "  help         - Show this help message"

ifeq ($(OS),Windows_NT)
    RM = rm -rf
else
    RM = rm -rf
endif
