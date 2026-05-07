# Makefile for pakka plugin.
# Passes referenced below are defined in DESIGN.md §10.

GO     ?= go
BIN    := bin/pakka-core
PKG    := ./cmd/pakka-core

.PHONY: help build cross test self-report clean

help:
	@echo "pakka — Claude Code harness"
	@echo ""
	@echo "Targets:"
	@echo "  build         Build pakka-core for current arch.         (Pass 1)"
	@echo "  cross         Build pakka-core for all release arches.   (Pass 5)"
	@echo "  test          Run Go unit tests.                          (Pass 1)"
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

self-report:
	@./bin/pakka-core-$$(uname -s | tr 'A-Z' 'a-z')-$$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/') report \
		--format=md --repo-root=.. > RECEIPTS.md
	@echo "RECEIPTS.md generated."

clean:
	rm -f bin/pakka-core bin/pakka-core.exe
