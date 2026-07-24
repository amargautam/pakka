// Package main provides the pakka-core CLI — the full pakka binary.
//
// pakka-core serves every subcommand, including recall (index/query), which
// links modernc.org/sqlite. The subcommand router and every command
// implementation live in internal/cli so the lean hot-path binary
// (cmd/pakka-hot) can reuse them without importing recall/sqlite.
//
// This main is intentionally thin: it wires the recall handlers (the only code
// that imports internal/recall, and therefore the only sqlite link) into the
// shared dispatcher, then delegates. See DESIGN.md §10 and
// docs/specs/2026-07-24-hot-path-startup-floor.md.
package main

import (
	"os"

	"github.com/amargautam/pakka/internal/cli"
)

func main() {
	// Wire recall (sqlite-linked) into the shared dispatcher. Only pakka-core
	// does this; pakka-hot leaves these nil so sqlite never links there.
	cli.IndexFunc = func(args []string) error { return runRecallIndex() }
	cli.QueryFunc = func(args []string) error { return runRecallQuery(args) }

	os.Exit(cli.Dispatch(os.Args))
}
