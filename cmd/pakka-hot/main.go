// Package main provides pakka-hot — the lean hot-path pakka binary.
//
// pakka-hot serves ONLY the three subcommands that fire on the session hot path:
//
//	guard        — PreToolUse allow/deny (fires on every Read/Write/Edit/Bash)
//	commit-gate  — PreToolUse Bash passthrough (fires on every Bash command)
//	status-line  — statusLine render
//
// It deliberately does NOT wire recall (cli.IndexFunc / cli.QueryFunc stay nil),
// so it never imports internal/recall and therefore never links
// modernc.org/sqlite. That drops the ~4ms package-init floor (the
// modernc.org/libc netdb table build) that the fat pakka-core pays on every
// hook invocation — bringing guard and commit-gate back inside their published
// latency budgets. bin/run dispatches these three subcommands to pakka-hot when
// a matching binary sits beside pakka-core, and falls back to pakka-core
// otherwise. See docs/specs/2026-07-24-hot-path-startup-floor.md.
//
// A guard test (cmd/pakka-hot/nosqlite_test.go) fails the build if any
// modernc.org/sqlite symbol appears in this binary's linked dependencies.
package main

import (
	"os"

	"github.com/amargautam/pakka/internal/hotcli"
)

func main() {
	os.Exit(hotcli.DispatchHot(os.Args))
}
