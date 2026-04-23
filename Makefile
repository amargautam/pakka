# Makefile for pakka plugin.
# Passes referenced below are defined in DESIGN.md §10.

GO     ?= go
BIN    := bin/pakka-core
PKG    := ./cmd/pakka-core

.PHONY: help build cross test bench self-report clean

help:
	@echo "pakka — Claude Code harness"
	@echo ""
	@echo "Targets:"
	@echo "  build         Build pakka-core for current arch.         (Pass 1)"
	@echo "  cross         Build pakka-core for all release arches.   (Pass 5)"
	@echo "  test          Run Go unit tests.                          (Pass 1)"
	@echo "  bench         Run v0 benchmark corpus end-to-end.         (Pass 5)"
	@echo "  self-report   Emit RECEIPTS.md from pakka's own audit.    (Pass 5)"
	@echo "  clean         Remove built binaries."

build:
	$(GO) build -o $(BIN) $(PKG)

cross:
	@echo "Pass 5 target. See DESIGN.md §10 Pass 5."
	@exit 1

test:
	$(GO) test ./...

bench:
	@echo "Pass 5 target. See DESIGN.md §9."
	@exit 1

self-report:
	@echo "Pass 5 target. See DESIGN.md §9.1."
	@exit 1

clean:
	rm -f bin/pakka-core bin/pakka-core.exe
