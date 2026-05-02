package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/amargautam/pakka/internal/audit"
	"github.com/amargautam/pakka/internal/commitgate"
	"github.com/amargautam/pakka/internal/compress/orchestrator"
	"github.com/amargautam/pakka/internal/meter"
	"github.com/amargautam/pakka/internal/statusline"
)

// CommitGateCmd implements the "commit-gate" subcommand.
type CommitGateCmd struct{}

func (c *CommitGateCmd) Name() string { return "commit-gate" }
func (c *CommitGateCmd) Run(args []string) error {
	runCommitGate()
	return nil
}

// --- commit-gate (Pass 3.1) ---

// settingsJSON mirrors the pakka section of settings.json for config loading.
type settingsJSON struct {
	Pakka struct {
		Signature *bool `json:"signature"`
		CoAuthor  *bool `json:"coAuthor"`
		Review    struct {
			ConfidenceThreshold *int     `json:"confidenceThreshold"`
			AutoGate            *bool    `json:"autoGate"`
			MaxDiffBytes        *int     `json:"maxDiffBytes"`
			SkipPaths           []string `json:"skipPaths"`
		} `json:"review"`
		Compress struct {
			Input               *bool    `json:"input"`
			Output              *bool    `json:"output"`
			OutputLevel         string   `json:"outputLevel"`
			ToolResult          *bool    `json:"toolResult"`
			ToolResultMaxBytes  *int     `json:"toolResultMaxBytes"`
			ToolResultHeadLines *int     `json:"toolResultHeadLines"`
			ToolResultTailLines *int     `json:"toolResultTailLines"`
			SubagentReturn      *bool    `json:"subagentReturn"`
			Semantic            *bool    `json:"semantic"`
			SemanticTargets     []string `json:"semanticTargets"`
			Engine              string   `json:"engine"`
		} `json:"compress"`
		Display struct {
			StatusLine *bool `json:"statusLine"`
		} `json:"display"`
		Recall struct {
			Enabled *bool `json:"enabled"`
		} `json:"recall"`
	} `json:"pakka"`
}

func runCommitGate() {
	event, ok := parseStrict(os.Stdin, os.Stderr)
	if !ok {
		os.Exit(1)
	}
	if event == nil {
		return // empty stdin — silent skip
	}

	if event.ToolName != "Bash" {
		return
	}

	var input struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(event.ToolInput, &input); err != nil {
		return // malformed input, don't block
	}

	cfg := loadCommitGateConfig()
	state := gatherReviewState(cfg)
	d := commitgate.Evaluate(input.Command, cfg, state)

	// Inject status trailer on allowed commits.
	if d.Allow && commitgate.IsGitCommit(input.Command) {
		level := loadOutputLevel()
		cgCWD := event.CWD
		if cgCWD == "" {
			cgCWD, _ = os.Getwd()
		}
		cgStale := orchestrator.CountStaleFromDisk(meter.RepoKey(cgCWD))
		summary := statusline.Summary(event, level, cgStale)
		target := d.Command
		if target == "" {
			target = input.Command
		}
		d.Command = commitgate.InjectTrailer(target, "pakka-session: "+summary)
	}

	// Audit note (skip events, oversize, etc.)
	if d.AuditNote != "" {
		_ = audit.WriteNote(event.SessionID, "commit_gate", d.AuditNote)
	}

	// Write verdict for auto-gate decisions on git commit commands
	if commitgate.IsGitCommit(input.Command) && cfg.AutoGate {
		writeVerdict(event.SessionID, d)
	}

	if !d.Allow {
		fmt.Fprint(os.Stderr, d.Stderr)
		os.Exit(2)
	}

	if d.Command != "" {
		_, _ = os.Stdout.Write(emitCommitRewrite(d.Command))
	}
}

// emitCommitRewrite returns the JSON envelope Claude Code expects for a
// PreToolUse Bash rewrite. The shape changed when Claude Code formalized the
// hook contract: callers MUST emit
//
//	{"hookSpecificOutput":{"hookEventName":"PreToolUse","updatedInput":{"command":"..."}}}
//
// Pre-Pass-4.7 pakka emitted the legacy `{"tool_input":{"command":"..."}}`
// shape. Claude Code silently ignored it, so every Claude-authored commit
// from the introduction of auto-trailers through Pass 4.6 landed without
// the Reviewed-by-pakka, Co-authored-by, or pakka-session trailers.
// Diagnostic: `git log` showed 0 trailers across the entire history despite
// the gate logging "passed" verdicts on every commit.
//
// Returns a complete line including the trailing newline emitted by
// json.Encoder so callers can write the bytes directly to stdout.
func emitCommitRewrite(cmd string) []byte {
	out := map[string]interface{}{
		"hookSpecificOutput": map[string]interface{}{
			"hookEventName": "PreToolUse",
			"updatedInput": map[string]string{
				"command": cmd,
			},
		},
	}
	b, _ := json.Marshal(out)
	return append(b, '\n')
}

func loadCommitGateConfig() *commitgate.Config {
	cfg := commitgate.DefaultConfig()

	root := pluginRoot()
	data, err := os.ReadFile(filepath.Join(root, "settings.json"))
	if err != nil {
		return cfg
	}

	var s settingsJSON
	if err := json.Unmarshal(data, &s); err != nil {
		return cfg
	}

	if s.Pakka.Signature != nil {
		cfg.Signature = *s.Pakka.Signature
	}
	if s.Pakka.CoAuthor != nil {
		cfg.CoAuthor = *s.Pakka.CoAuthor
	}
	if s.Pakka.Review.ConfidenceThreshold != nil {
		cfg.ConfidenceThreshold = *s.Pakka.Review.ConfidenceThreshold
	}
	if s.Pakka.Review.AutoGate != nil {
		cfg.AutoGate = *s.Pakka.Review.AutoGate
	}
	if s.Pakka.Review.MaxDiffBytes != nil {
		cfg.MaxDiffBytes = *s.Pakka.Review.MaxDiffBytes
	}
	if len(s.Pakka.Review.SkipPaths) > 0 {
		cfg.SkipPaths = s.Pakka.Review.SkipPaths
	}

	return cfg
}

// repoRoot returns the absolute git repo root for the current working
// directory, or "" if git cannot resolve one. Callers pass the result to
// `git -C <root> ...` so commands behave the same regardless of which
// subdirectory the hook is invoked from.
func repoRoot() string {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func gatherReviewState(cfg *commitgate.Config) *commitgate.State {
	state := &commitgate.State{}

	// Resolve repo root so the diff works when the hook runs from a
	// subdirectory. Without -C <root>, a CWD outside or below the repo
	// boundary can yield an empty diff and cause the gate to block a
	// legitimate commit.
	root := repoRoot()

	// Diff size via git
	diffArgs := []string{}
	if root != "" {
		diffArgs = append(diffArgs, "-C", root)
	}
	diffArgs = append(diffArgs, "diff", "--cached")
	out, err := exec.Command("git", diffArgs...).Output()
	if err == nil {
		state.DiffBytes = len(out)
	}

	// Recent pass check
	data, err := os.ReadFile(".pakka/reviews/last-pass-ts")
	if err == nil {
		ts, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
		if err == nil && time.Since(time.Unix(ts, 0)) < 300*time.Second {
			state.HasRecentPass = true
		}
	}

	// Load error findings from latest review (only if no recent pass).
	// Filter by the changed-line set so pre-existing-code findings cannot
	// block a commit that doesn't touch those lines. The unfiltered
	// findings remain on disk (.pakka/reviews/<id>.jsonl) for debugging.
	if !state.HasRecentPass {
		state.ErrorFindings = loadLatestErrors(cfg.ConfidenceThreshold, scopeFromStagedDiff())
	}

	return state
}

// scopeFromStagedDiff returns the (file, line) set of additions/modifications
// in the staged diff, used to scope review findings to changed lines only.
// Returns an empty (non-nil) Scope on git failure or empty diff — the
// resulting filter drops everything, which is the safe default for the gate
// (no scope → no findings can fire → no false-positive block).
func scopeFromStagedDiff() commitgate.Scope {
	out, err := exec.Command("git", "diff", "--cached", "--unified=0").Output()
	if err != nil {
		return commitgate.Scope{}
	}
	return commitgate.ChangedLines(string(out))
}

// loadLatestErrors reads the most recent findings file from .pakka/reviews/.
// Naming convention:
//   - verdict-*.jsonl — written by commit-gate, contains pass/fail verdicts
//   - *.jsonl (without verdict- prefix) — written by /pakka:review, contains findings
//
// This function only reads findings files (skips verdict-* files). It applies
// two filters: (1) severity=error AND confidence >= threshold, (2) (file, line)
// must be in scope (staged-diff change set). Findings outside scope come from
// pre-existing code that the current commit does not touch and must not block
// it. The on-disk file is left intact so the audit trail keeps the unfiltered
// findings for debugging.
func loadLatestErrors(threshold int, scope commitgate.Scope) []commitgate.Finding {
	entries, err := os.ReadDir(".pakka/reviews")
	if err != nil {
		return nil
	}

	var latest string
	var latestTime time.Time
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		// Skip verdict files — they contain pass/fail verdicts, not findings.
		if strings.HasPrefix(e.Name(), "verdict-") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(latestTime) {
			latestTime = info.ModTime()
			latest = e.Name()
		}
	}
	if latest == "" {
		return nil
	}

	data, err := os.ReadFile(filepath.Join(".pakka", "reviews", latest))
	if err != nil {
		return nil
	}

	var findings []commitgate.Finding
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var f commitgate.Finding
		if json.Unmarshal([]byte(line), &f) != nil {
			continue
		}
		if f.Severity == "error" && f.Confidence >= threshold {
			findings = append(findings, f)
		}
	}
	// Scope filter: drop findings on lines the staged diff does not touch.
	return commitgate.Filter(findings, scope)
}

// writeVerdict writes a verdict file to .pakka/reviews/.
// Naming convention: verdict-<timestamp>.jsonl — distinguishes from findings files
// written by /pakka:review (which use <sha-or-ts>.jsonl without a prefix).
func writeVerdict(sessionID string, d *commitgate.Decision) {
	dir := ".pakka/reviews"
	_ = os.MkdirAll(dir, 0755)

	ts := strconv.FormatInt(time.Now().Unix(), 10)
	verdict := "passed"
	if !d.Allow {
		verdict = "failed"
	}

	entry := map[string]interface{}{
		"ts":      time.Now().UTC().Format(time.RFC3339),
		"session": sessionID,
		"verdict": verdict,
	}

	data, _ := json.Marshal(entry)
	_ = os.WriteFile(filepath.Join(dir, "verdict-"+ts+".jsonl"), append(data, '\n'), 0644)
}
