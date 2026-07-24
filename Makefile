# Makefile for pakka plugin.
# Passes referenced below are defined in DESIGN.md §10.

GO     ?= go
BIN    := bin/pakka-core
PKG    := ./cmd/pakka-core
HOTBIN := bin/pakka-hot
HOTPKG := ./cmd/pakka-hot

.PHONY: help build cross release test test-js bench bench-latency self-report clean

# Compression levels measured by `make bench`. Override: make bench BENCH_LEVELS=ultra
BENCH_LEVELS ?= lite strict ultra super-ultra

help:
	@echo "pakka — Claude Code harness"
	@echo ""
	@echo "Targets:"
	@echo "  build         Build pakka-core for current arch.         (Pass 1)"
	@echo "  cross         Build pakka-core for all release arches.   (Pass 5)"
	@echo "  release       Reproducible cross-build (clean tree only) + SHA256SUMS."
	@echo "  test          Run Go unit tests.                          (Pass 1)"
	@echo "  test-js       Run JS hook tests (node --test)."
	@echo "  bench         Run A/B benchmark via claude -p OAuth.      (issue #13)"
	@echo "  bench-latency Measure hook hot-path latency (p50/p95).    (v0.12.0)"
	@echo "  self-report   Emit RECEIPTS.md from pakka's own audit.    (Pass 5)"
	@echo "  clean         Remove built binaries."

build:
	$(GO) build -o $(BIN) $(PKG)
	$(GO) build -o $(HOTBIN) $(HOTPKG)

cross:
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 $(GO) build -trimpath -o bin/pakka-core-darwin-arm64 $(PKG)
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 $(GO) build -trimpath -o bin/pakka-core-darwin-amd64 $(PKG)
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 $(GO) build -trimpath -o bin/pakka-core-linux-arm64 $(PKG)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GO) build -trimpath -o bin/pakka-core-linux-amd64 $(PKG)
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 $(GO) build -trimpath -o bin/pakka-core-windows-amd64.exe $(PKG)
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 $(GO) build -trimpath -o bin/pakka-hot-darwin-arm64 $(HOTPKG)
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 $(GO) build -trimpath -o bin/pakka-hot-darwin-amd64 $(HOTPKG)
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 $(GO) build -trimpath -o bin/pakka-hot-linux-arm64 $(HOTPKG)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GO) build -trimpath -o bin/pakka-hot-linux-amd64 $(HOTPKG)
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 $(GO) build -trimpath -o bin/pakka-hot-windows-amd64.exe $(HOTPKG)

# release — reproducible cross-build with provenance stamping.
#
# Output dir defaults to bin/ (RELEASE_DIR=dist in CI — see below). Refuses to
# run on a dirty working tree so every binary stamps vcs.modified=false and
# vcs.revision=<HEAD-at-build> (see `go version -m`). Binaries are built to a
# temp dir *outside* the repo — that keeps the source tree clean while Go reads
# git status for each build, so binary N landing in the output dir never taints
# binary N+1's stamp. All five are moved in at once, then SHA256SUMS is written
# and provenance printed.
#
# Reproducibility: -trimpath strips local paths, CGO_ENABLED=0 removes the C
# toolchain. Same Go toolchain + same commit => byte-identical binaries. A
# verifier reproduces a shipped binary by checking out the *revision the binary
# reports* (`go version -m`), not necessarily the tag — the committed in-tree
# binaries embed the commit they were built at (the parent of the commit that
# carries them; a binary cannot embed its own carrying commit's hash). CI builds
# fresh assets at the tag into a gitignored dir, so its vcs.revision is the tag
# commit and `git status` stays clean.
RELEASE_DIR ?= bin

release:
	@if [ -n "$$(git status --porcelain)" ]; then \
		echo "release: working tree not clean — commit or stash first:"; \
		git status --porcelain; \
		exit 1; \
	fi
	@rev=$$(git rev-parse HEAD); \
	mkdir -p "$(RELEASE_DIR)"; \
	tmp=$$(mktemp -d); \
	echo "Building reproducible binaries at $$rev -> $(RELEASE_DIR)/ ..."; \
	GOOS=darwin  GOARCH=arm64 CGO_ENABLED=0 $(GO) build -trimpath -o $$tmp/pakka-core-darwin-arm64     $(PKG) || exit 1; \
	GOOS=darwin  GOARCH=amd64 CGO_ENABLED=0 $(GO) build -trimpath -o $$tmp/pakka-core-darwin-amd64     $(PKG) || exit 1; \
	GOOS=linux   GOARCH=arm64 CGO_ENABLED=0 $(GO) build -trimpath -o $$tmp/pakka-core-linux-arm64      $(PKG) || exit 1; \
	GOOS=linux   GOARCH=amd64 CGO_ENABLED=0 $(GO) build -trimpath -o $$tmp/pakka-core-linux-amd64      $(PKG) || exit 1; \
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 $(GO) build -trimpath -o $$tmp/pakka-core-windows-amd64.exe $(PKG) || exit 1; \
		GOOS=darwin  GOARCH=arm64 CGO_ENABLED=0 $(GO) build -trimpath -o $$tmp/pakka-hot-darwin-arm64      $(HOTPKG) || exit 1; \
		GOOS=darwin  GOARCH=amd64 CGO_ENABLED=0 $(GO) build -trimpath -o $$tmp/pakka-hot-darwin-amd64      $(HOTPKG) || exit 1; \
		GOOS=linux   GOARCH=arm64 CGO_ENABLED=0 $(GO) build -trimpath -o $$tmp/pakka-hot-linux-arm64       $(HOTPKG) || exit 1; \
		GOOS=linux   GOARCH=amd64 CGO_ENABLED=0 $(GO) build -trimpath -o $$tmp/pakka-hot-linux-amd64       $(HOTPKG) || exit 1; \
		GOOS=windows GOARCH=amd64 CGO_ENABLED=0 $(GO) build -trimpath -o $$tmp/pakka-hot-windows-amd64.exe  $(HOTPKG) || exit 1; \
	mv $$tmp/pakka-core-* $$tmp/pakka-hot-* "$(RELEASE_DIR)"/; \
	rmdir $$tmp; \
	( cd "$(RELEASE_DIR)" && shasum -a 256 \
		pakka-core-darwin-arm64 pakka-core-darwin-amd64 \
		pakka-core-linux-arm64 pakka-core-linux-amd64 \
		pakka-core-windows-amd64.exe \
		pakka-hot-darwin-arm64 pakka-hot-darwin-amd64 \
		pakka-hot-linux-arm64 pakka-hot-linux-amd64 \
		pakka-hot-windows-amd64.exe > SHA256SUMS ); \
	echo ""; echo "$(RELEASE_DIR)/SHA256SUMS:"; cat "$(RELEASE_DIR)/SHA256SUMS"; \
	echo ""; echo "Provenance (want vcs.modified=false, vcs.revision=$$rev):"; \
	for b in pakka-core-darwin-arm64 pakka-core-darwin-amd64 pakka-core-linux-arm64 pakka-core-linux-amd64 pakka-core-windows-amd64.exe \
		pakka-hot-darwin-arm64 pakka-hot-darwin-amd64 pakka-hot-linux-arm64 pakka-hot-linux-amd64 pakka-hot-windows-amd64.exe; do \
		echo "== $$b =="; \
		$(GO) version -m "$(RELEASE_DIR)/$$b" | grep -E 'vcs.revision|vcs.modified'; \
	done

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

# bench-latency — hook hot-path wall-clock latency (spec: v0.12.0 consolidation
# acceptance 5+6). Builds a fresh binary, feeds each hook realistic event JSON
# on stdin, reports p50/p95 vs budget. Writes benchmarks/latency-v0.17.0.md.
# Script exit: 0 = all budgets pass, 1 = a budget is over (report still
# written — commit-gate <5ms is a documented miss deferred to #17), 2 = broken
# harness (numbers stopped varying with input). Only 2 fails the target.
bench-latency:
	@python3 benchmarks/latency_bench.py --runs 50 --out benchmarks/latency-v0.17.0.md; \
	ec=$$?; \
	if [ $$ec -eq 2 ]; then echo "bench-latency: harness broken — see above"; exit 1; fi; \
	if [ $$ec -eq 1 ]; then echo "bench-latency: report written; a budget is over (documented in report)"; fi; \
	exit 0

self-report:
	@./bin/pakka-core-$$(uname -s | tr 'A-Z' 'a-z')-$$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/') report \
		--format=md --repo-root=.. > RECEIPTS.md
	@echo "RECEIPTS.md generated."

clean:
	rm -f bin/pakka-core bin/pakka-core.exe bin/pakka-hot bin/pakka-hot.exe
