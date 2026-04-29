// Orchestrator dispatch + status command for pakka-core. Kept in a separate
// file so the cross-package wiring (settings → orchestrator.Run + AsyncCommand)
// stays readable next to the rest of main.go.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/amargautam/pakka/internal/compress/orchestrator"
	"github.com/amargautam/pakka/internal/compress/semantic"
	"github.com/amargautam/pakka/internal/meter"
)

// orchestratorTargets returns the configured allowlist for the running
// pakka-core, falling back to the orchestrator package's defaults.
func orchestratorTargets() []string {
	s := loadSettings()
	if len(s.Pakka.Compress.SemanticTargets) > 0 {
		return s.Pakka.Compress.SemanticTargets
	}
	return orchestrator.DefaultTargets
}

// orchestratorEnabled returns whether the SessionStart auto-orchestrator is
// allowed to run. It requires (a) compress.semantic=true in settings AND
// (b) at least one target. ANTHROPIC_API_KEY absence is handled separately —
// the orchestrator silently no-ops without one.
func orchestratorEnabled() bool {
	s := loadSettings()
	if s.Pakka.Compress.Semantic == nil || !*s.Pakka.Compress.Semantic {
		return false
	}
	if len(orchestratorTargets()) == 0 {
		return false
	}
	return true
}

// forkOrchestrator spawns the detached background child for SessionStart.
// Returns immediately — the SessionStart hook MUST stay under 50ms.
func forkOrchestrator(repo, level, sessionID string) {
	o := newOrchestrator(repo, level, sessionID)
	if o == nil {
		return
	}
	debugLogf("orchestrator: forking bg for repo=%s level=%s", repo, level)
	o.RunAsync()
}

// runOrchestrator is invoked when --orchestrator-bg or --orchestrator-run is
// set. Both run the same synchronous walk; the difference is whether the
// process was forked or invoked directly.
func runOrchestrator(repo, level string) {
	if repo == "" {
		repo, _ = os.Getwd()
	}
	repo = meter.RepoKey(repo)
	if level == "" {
		level = loadOutputLevel()
	}
	o := newOrchestrator(repo, level, fmt.Sprintf("orch-%d", os.Getpid()))
	if o == nil {
		debugLogf("orchestrator: refusing to run repo=%s level=%s (no api key or no targets)", repo, level)
		return
	}
	if err := o.Run(context.Background()); err != nil {
		debugLogf("orchestrator: run repo=%s err=%v", repo, err)
	}
}

// newOrchestrator builds an Orchestrator wired to the production Anthropic
// rewriter. Returns nil when the API key is missing — caller logs and skips.
func newOrchestrator(repo, level, sessionID string) *orchestrator.Orchestrator {
	if repo == "" {
		return nil
	}
	client, ok := semantic.NewAnthropicClient()
	if !ok {
		// Note: callers may still want to construct without a rewriter for
		// dry-run paths; today every invocation needs the rewriter, so we
		// bail and log.
		return nil
	}
	return &orchestrator.Orchestrator{
		Repo:      repo,
		Targets:   orchestratorTargets(),
		Level:     level,
		SessionID: sessionID,
		Rewriter:  client,
	}
}

// runOrchestratorStatus prints the orchestrator state file as a table.
//
// Usage: pakka-core orchestrator-status [--repo=<dir>]
//
// Purpose: Lets users introspect what was compressed, when, at what level,
// and whether the validator passed. Drives troubleshooting for the stale
// glyph in the status-line.
// Errors: Exits 1 only on filesystem failure; missing state prints "no state".
func runOrchestratorStatus() {
	repo := ""
	for _, a := range os.Args[2:] {
		if len(a) > 7 && a[:7] == "--repo=" {
			repo = a[7:]
		}
	}
	if repo == "" {
		repo, _ = os.Getwd()
	}
	repo = meter.RepoKey(repo)
	state, err := orchestrator.LoadState(repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pakka: orchestrator-status: %v\n", err)
		os.Exit(1)
	}
	all := state.All()
	if len(all) == 0 {
		fmt.Fprintf(os.Stdout, "pakka orchestrator: no state at %s\n",
			filepath.Join(repo, ".pakka", orchestrator.StateFileName))
		return
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "FILE\tLEVEL\tCOMPRESSED-AT\tOK")
	for _, e := range all {
		ok := "yes"
		if !e.Entry.ValidatorPasses {
			ok = "NO"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", e.Path, e.Entry.Level, e.Entry.CompressedAt, ok)
	}
	_ = tw.Flush()
}
