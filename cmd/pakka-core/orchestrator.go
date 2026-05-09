// Orchestrator dispatch + status command for pakka-core. Kept in a separate
// file so the cross-package wiring (settings → orchestrator.Run + AsyncCommand)
// stays readable next to the rest of main.go.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/tabwriter"

	"github.com/amargautam/pakka/internal/compress/orchestrator"
	"github.com/amargautam/pakka/internal/compress/semantic"
	"github.com/amargautam/pakka/internal/meter"
)

// semanticEnabled returns whether semantic orchestration should run for the
// given level and explicit user setting.
//
// Rules:
//   - "super-ultra": always true — enforced, cannot be disabled
//   - "ultra": true unless explicitly disabled (explicit == false pointer)
//   - "lite", "strict", or unknown: false unless explicitly enabled
func semanticEnabled(level string, explicit *bool) bool {
	switch level {
	case "super-ultra":
		return true // enforced — cannot be disabled
	case "ultra":
		if explicit != nil && !*explicit {
			return false // opt-out
		}
		return true // default on
	default:
		if explicit != nil && *explicit {
			return true // explicit opt-in
		}
		return false
	}
}

// lookPath wraps exec.LookPath so tests can stub PATH resolution without
// mutating $PATH (which would race other parallel tests).
var lookPath = exec.LookPath

// rewriterEngine names the auth path the orchestrator will use.
type rewriterEngine string

const (
	engineClaudeCLI    rewriterEngine = "claude-cli"
	engineAnthropicHTTP rewriterEngine = "anthropic-http"
	engineAuto         rewriterEngine = "auto"
	engineNone         rewriterEngine = ""
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
// allowed to run. It requires (a) semanticEnabled check passes AND
// (b) at least one target. Auth (claude CLI or ANTHROPIC_API_KEY) is checked
// later in resolveRewriter — the orchestrator silently no-ops without one.
func orchestratorEnabled() bool {
	s := loadSettings()
	level := loadOutputLevel()
	if !semanticEnabled(level, s.Pakka.Compress.Semantic) {
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
	if err := o.RunAsync(); err != nil {
		debugLogf("orchestrator: fork failed: %v", err)
	}
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
		debugLogf("orchestrator: refusing to run repo=%s level=%s (no rewriter or no targets)", repo, level)
		return
	}
	if err := o.Run(context.Background()); err != nil {
		debugLogf("orchestrator: run repo=%s err=%v", repo, err)
	}
}

// newOrchestrator builds an Orchestrator wired to the best available
// rewriter. Resolution order:
//
//  1. `pakka.compress.engine` setting (forces a path; useful for debugging).
//  2. `claude` CLI on PATH → ClaudeCLI rewriter (zero-config OAuth path).
//  3. ANTHROPIC_API_KEY set → AnthropicClient HTTP rewriter (legacy).
//  4. Neither → nil (caller logs and skips).
//
// Logs to ~/.pakka/orchestrator.log which path was tried and why each
// failed/succeeded. Never logs file contents — only paths, env-var presence
// (boolean), and selected engine name.
func newOrchestrator(repo, level, sessionID string) *orchestrator.Orchestrator {
	if repo == "" {
		return nil
	}
	rewriter := resolveRewriter()
	if rewriter == nil {
		return nil
	}
	return &orchestrator.Orchestrator{
		Repo:      repo,
		Targets:   orchestratorTargets(),
		Level:     level,
		SessionID: sessionID,
		Rewriter:  rewriter,
	}
}

// resolveRewriter returns the best Rewriter for current settings + env, or
// nil when neither auth path is available.
//
// Purpose: Single source of truth for the "which auth path?" decision. Used
// by both the orchestrator wiring and the inline semantic path in
// runCompress (so /pakka:compress and SessionStart pick the same engine).
// Errors: Never errors. Falls through to nil with a log line.
func resolveRewriter() semantic.Rewriter {
	engine := configuredEngine()
	switch engine {
	case engineClaudeCLI:
		if r := tryClaudeCLI(); r != nil {
			debugLogf("orchestrator: engine=claude-cli (forced via settings)")
			return r
		}
		debugLogf("orchestrator: engine=claude-cli forced but `claude` not on PATH; refusing fallback")
		return nil
	case engineAnthropicHTTP:
		if r := tryAnthropicHTTP(); r != nil {
			debugLogf("orchestrator: engine=anthropic-http (forced via settings)")
			return r
		}
		debugLogf("orchestrator: engine=anthropic-http forced but ANTHROPIC_API_KEY missing; refusing fallback")
		return nil
	}
	// engineAuto / unset: prefer CLI, fall back to HTTP.
	if r := tryClaudeCLI(); r != nil {
		debugLogf("orchestrator: engine=claude-cli (auto)")
		return r
	}
	if r := tryAnthropicHTTP(); r != nil {
		debugLogf("orchestrator: engine=anthropic-http (auto, claude CLI not on PATH)")
		return r
	}
	debugLogf("orchestrator: no rewriter available — `claude` not on PATH and ANTHROPIC_API_KEY unset")
	return nil
}

// configuredEngine reads pakka.compress.engine and normalizes it.
func configuredEngine() rewriterEngine {
	s := loadSettings()
	switch s.Pakka.Compress.Engine {
	case "claude-cli":
		return engineClaudeCLI
	case "anthropic-http":
		return engineAnthropicHTTP
	case "auto", "":
		return engineAuto
	default:
		return engineAuto
	}
}

// tryClaudeCLI returns a ClaudeCLI rewriter when `claude` is on PATH.
func tryClaudeCLI() semantic.Rewriter {
	if _, err := lookPath("claude"); err != nil {
		return nil
	}
	return semantic.NewClaudeCLI()
}

// tryAnthropicHTTP returns an AnthropicClient when ANTHROPIC_API_KEY is set.
func tryAnthropicHTTP() semantic.Rewriter {
	c, ok := semantic.NewAnthropicClient()
	if !ok {
		return nil
	}
	return c
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
