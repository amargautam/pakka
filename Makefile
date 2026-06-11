# Makefile for pakka plugin.
# Passes referenced below are defined in DESIGN.md §10.

GO     ?= go
BIN    := bin/pakka-core
PKG    := ./cmd/pakka-core

.PHONY: help build cross test test-js bench self-report clean

# Compression levels measured by `make bench`. Override: make bench BENCH_LEVELS=ultra
BENCH_LEVELS ?= lite strict ultra super-ultra

help:
	@echo "pakka — Claude Code harness"
	@echo ""
	@echo "Targets:"
	@echo "  build         Build pakka-core for current arch.         (Pass 1)"
	@echo "  cross         Build pakka-core for all release arches.   (Pass 5)"
	@echo "  test          Run Go unit tests.                          (Pass 1)"
	@echo "  test-js       Run JS hook tests (node --test)."
	@echo "  bench         Run A/B benchmark via claude -p OAuth.      (issue #13)"
	@echo "  self-report   Emit RECEIPTS.md from pakka's own audit.    (Pass 5)"
	@echo "  clean         Remove built binaries."

build:
	$(GO) build -o $(BIN) $(PKG)

cross:
	GOOS=darwin GOARCH=arm64 $(GO) build -o bin/pakka-core-darwin-arm64 $(PKG)
	GOOS=darwin GOARCH=amd64 $(GO) build -o bin/pakka-core-darwin-amd64 $(PKG)
	GOOS=linux GOARCH=arm64 $(GO) build -o bin/pakka-core-linux-arm64 $(PKG)
	GOOS=linux GOARCH=amd64 $(GO) build -o bin/pakka-core-linux-amd64 $(PKG)
	GOOS=windows GOARCH=amd64 $(GO) build -o bin/pakka-core-windows-amd64.exe $(PKG)

test:
	$(GO) test ./...

test-js:
	node --test hooks/*.test.js

# bench — A/B benchmark over benchmarks/corpus.json (issue #13).
#
# Both arms run through `claude -p` with the inherited OAuth session — NO
# API key is used or required. Raw arm sets PAKKA_DISABLED=1 (kill-switch in
# bin/run + JS hooks); pakka arm pins PAKKA_DEFAULT_LEVEL per level.
# Requires: claude CLI logged in, pakka plugin (>= this commit) installed.
#
# Results land in benchmarks/results/<stamp>-<level>.json, each carrying a
# measured_output_ratio per level — commit them as provenance artifacts and
# use them to replace the constant outputMultiplier table in
# internal/statusline (see TODO there).
bench: build
	@echo "Running A/B benchmark via claude -p (inherited OAuth — no API key)..."
	@mkdir -p benchmarks/results
	@stamp=$$(date +%Y%m%d-%H%M%S); \
	for lvl in $(BENCH_LEVELS); do \
		echo "== level: $$lvl =="; \
		./bin/pakka-core bench \
			--corpus=benchmarks/corpus.json \
			--out=benchmarks/results/$$stamp-$$lvl.json \
			--level=$$lvl --verbose || exit 1; \
	done; \
	echo "Results: benchmarks/results/$$stamp-*.json — commit them and update claim numbers."

self-report:
	@./bin/pakka-core-$$(uname -s | tr 'A-Z' 'a-z')-$$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/') report \
		--format=md --repo-root=.. > RECEIPTS.md
	@echo "RECEIPTS.md generated."

clean:
	rm -f bin/pakka-core bin/pakka-core.exe
