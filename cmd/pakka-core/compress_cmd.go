package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/amargautam/pakka/internal/compress"
	"github.com/amargautam/pakka/internal/compress/semantic"
	"github.com/amargautam/pakka/internal/hookevent"
	"github.com/amargautam/pakka/internal/meter"
)

// CompressCmd implements the "compress" subcommand.
type CompressCmd struct{}

func (c *CompressCmd) Name() string { return "compress" }
func (c *CompressCmd) Run(args []string) error {
	runCompress()
	return nil
}

func runCompress() {
	mode := "strict"
	phase := ""
	semanticFlag := false
	levelStr := ""
	orchestratorBg := false
	orchestratorRun := false
	repoFlag := ""
	for _, a := range os.Args[2:] {
		switch {
		case strings.HasPrefix(a, "--mode="):
			mode = strings.TrimPrefix(a, "--mode=")
		case strings.HasPrefix(a, "--phase="):
			phase = strings.TrimPrefix(a, "--phase=")
		case strings.HasPrefix(a, "--level="):
			levelStr = strings.TrimPrefix(a, "--level=")
		case strings.HasPrefix(a, "--repo="):
			repoFlag = strings.TrimPrefix(a, "--repo=")
		case a == "--semantic":
			semanticFlag = true
		case a == "--orchestrator-bg":
			orchestratorBg = true
		case a == "--orchestrator-run":
			orchestratorRun = true
		}
	}

	// Orchestrator entry points: --orchestrator-bg is the body of the forked
	// detached child; --orchestrator-run is the synchronous re-compress walk
	// driven by /pakka:compress <level>.
	if orchestratorBg || orchestratorRun {
		runOrchestrator(repoFlag, levelStr)
		return
	}

	// Default level when --semantic is set without --level.
	// "ultra" is pakka's brand default — fewer tokens by default.
	if semanticFlag && levelStr == "" {
		levelStr = "ultra"
	}
	if semanticFlag {
		mode = "semantic"
	}

	var input string
	var sessionID string

	if phase != "" {
		// Hook invocation: parse event JSON from stdin
		event := parseLenient(os.Stdin)
		sessionID = event.SessionID
		switch phase {
		case "tool-result":
			runCompressToolResult(event)
			return
		case "subagent-return":
			runCompressSubagentReturn(event)
			return
		case "session-start":
			// Per-vector gate: skip entirely if input compression disabled.
			if !isInputEnabled() {
				return
			}
			// Auto-compress CLAUDE.md, DESIGN.md, BUILD.md in CWD + one level deep.
			cwd := event.CWD
			if cwd == "" {
				cwd, _ = os.Getwd()
			}
			debugLogf("compress cwd=%s event.cwd=%s", cwd, event.CWD)
			// session-start auto-compress always uses deterministic strict —
			// LLM calls during SessionStart hooks would block the editor.
			autoCompressContextFiles(cwd, "strict", sessionID)
			// Pass 4.3: when semantic auto-orchestration is enabled, fork a
			// detached child to walk the allowlist with the LLM rewriter.
			// The fork is intentionally fire-and-forget — the SessionStart
			// hook MUST return in <50ms.
			if orchestratorEnabled() {
				repo := meter.RepoKey(cwd)
				forkOrchestrator(repo, loadOutputLevel(), sessionID)
			}
			return
		}
	} else {
		// Skill/direct invocation: raw text on stdin
		data, _ := io.ReadAll(os.Stdin)
		input = string(data)
		sessionID = fmt.Sprintf("sess-%d", os.Getpid())
	}

	if input == "" {
		return
	}

	m := compress.ParseMode(mode)

	// Semantic path: try the LLM rewriter; fall back to deterministic strict
	// when no auth path is available. Never fail the call on missing auth.
	//
	// Resolution: prefer `claude -p` subprocess (reuses Claude Code OAuth so
	// no API key is required); fall back to ANTHROPIC_API_KEY HTTP; then
	// finally to deterministic strict. See orchestrator.go: resolveRewriter.
	if m == compress.ModeSemantic {
		level := semantic.ParseLevel(levelStr)
		rewriter := resolveRewriter()
		if rewriter == nil {
			debugLogf("compress semantic: no rewriter available (claude CLI absent and ANTHROPIC_API_KEY unset), falling back to deterministic strict (level=%s, bytes=%d)", level, len(input))
			fallback := compress.Run(input, compress.ModeStrict)
			emitCompressResult(fallback, sessionID)
			return
		}
		result, err := compress.RunSemantic(input, compress.SemanticOptions{
			Rewriter: rewriter,
			Level:    level,
		})
		if err != nil {
			// Validator failed after retries — result.Output is the original
			// input unchanged (safety contract). Log and emit the original.
			debugLogf("compress semantic: validator failed level=%s bytes=%d err=%v", level, len(input), err)
		}
		emitCompressResult(result, sessionID)
		return
	}

	result := compress.Run(input, m)
	emitCompressResult(result, sessionID)
}

// emitCompressResult writes the compressed output to stdout, the ratio note
// to stderr, and meters the savings.
func emitCompressResult(result *compress.Result, sessionID string) {
	if result == nil {
		return
	}
	fmt.Fprint(os.Stdout, result.Output)
	fmt.Fprintf(os.Stderr, "pakka: %s\n", compress.FormatRatio(result))

	saved := int64(result.OriginalSize - result.CompressedSize)
	if saved > 0 {
		cwd, _ := os.Getwd()
		_ = meter.WriteSavings(sessionID, meter.RepoKey(cwd), saved)
	}
}

// collectContextPaths returns candidate CLAUDE.md/DESIGN.md/BUILD.md paths
// in dir and its immediate subdirectories.
func collectContextPaths(dir string) []string {
	targets := []string{"CLAUDE.md", "DESIGN.md", "BUILD.md"}
	var paths []string
	for _, name := range targets {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			paths = append(paths, p)
		}
	}
	entries, err := os.ReadDir(dir)
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			for _, name := range targets {
				p := filepath.Join(dir, e.Name(), name)
				if _, err := os.Stat(p); err == nil {
					paths = append(paths, p)
				}
			}
		}
	}
	return paths
}

// trustedFallbackDir reports whether dir is safe to scan as a project-root
// fallback when the original CWD has no context files. Trusted = either
// inside the user's home directory, OR carries a project sentinel (a .git
// or .pakka entry). Anything else (e.g., /tmp, /, another user's home) is
// rejected so a crafted CWD cannot trick us into rewriting unrelated
// CLAUDE.md/DESIGN.md files in an ancestor we never owned.
func trustedFallbackDir(dir string) bool {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		homeAbs, err := filepath.Abs(home)
		if err == nil {
			rel, err := filepath.Rel(homeAbs, abs)
			if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
				return true
			}
		}
	}
	for _, sentinel := range []string{".git", ".pakka"} {
		if _, err := os.Stat(filepath.Join(abs, sentinel)); err == nil {
			return true
		}
	}
	return false
}

// autoCompressContextFiles finds CLAUDE.md, DESIGN.md, BUILD.md in dir and
// one level of subdirectories. For each file without an existing .original.md
// backup, it compresses in place, writes the backup, and records savings.
// If no candidates are found and dir is not /, it retries with the parent directory.
func autoCompressContextFiles(dir, mode, sessionID string) {
	m := compress.ParseMode(mode)
	repo := meter.RepoKey(dir)

	// Collect candidate paths: root + immediate subdirectories.
	paths := collectContextPaths(dir)
	debugLogf("compress: dir=%s candidates=%d", dir, len(paths))

	// Fallback: if no candidates found and we're not at filesystem root,
	// try the parent directory. Handles cases where CWD is a subdirectory
	// of the project root (e.g., hook runner sets CWD to a child dir).
	//
	// Boundary check: a crafted CWD in a hook event could otherwise walk us
	// into ancestor directories where unrelated CLAUDE.md/DESIGN.md files
	// live (e.g., /Users/<other>/Projects). Only follow the fallback when
	// the parent is either inside the user's home dir OR carries a project
	// sentinel (.git or .pakka). Otherwise skip — better to no-op than to
	// rewrite ancestor context files.
	if len(paths) == 0 && dir != "/" {
		parent := filepath.Dir(dir)
		if parent != dir && trustedFallbackDir(parent) {
			paths = collectContextPaths(parent)
			debugLogf("compress: fallback dir=%s candidates=%d", parent, len(paths))
		}
	}

	for _, p := range paths {
		stem := strings.TrimSuffix(p, filepath.Ext(p))
		backupPath := stem + ".original.md"
		if info, err := os.Stat(backupPath); err == nil {
			// Already compressed — meter the savings delta.
			backupSize := info.Size()
			if ci, err := os.Stat(p); err == nil {
				saved := backupSize - ci.Size()
				if saved > 0 {
					_ = meter.WriteSavings(sessionID, repo, saved)
				}
			}
			continue
		}

		content, err := os.ReadFile(p)
		if err != nil {
			continue
		}

		result := compress.Run(string(content), m)
		if result.Ratio < 1.0 {
			continue // not worth it
		}

		// Write backup of original content.
		if err := os.WriteFile(backupPath, content, 0644); err != nil {
			continue
		}

		// Overwrite with compressed version.
		if err := os.WriteFile(p, []byte(result.Output), 0644); err != nil {
			continue
		}

		saved := int64(result.OriginalSize - result.CompressedSize)
		if saved > 0 {
			_ = meter.WriteSavings(sessionID, repo, saved)
		}

		fmt.Fprintf(os.Stderr, "pakka: compressed %s — %.0f%% (%s → %s)\n",
			filepath.Base(p), result.Ratio,
			compress.FmtSize(result.OriginalSize), compress.FmtSize(result.CompressedSize))
	}
}

// --- compress --phase=tool-result (Pass 4.1) ---

// loadToolResultConfig returns tool result compression configuration.
//
// Purpose: Load threshold and line counts for tool result truncation.
// Errors: Never errors; returns defaults on any failure.
func loadToolResultConfig() (maxBytes, headLines, tailLines int) {
	maxBytes = 10240 // 10KB default
	headLines = 80
	tailLines = 20

	s := loadSettings()
	if s.Pakka.Compress.ToolResultMaxBytes != nil {
		maxBytes = *s.Pakka.Compress.ToolResultMaxBytes
	}
	if s.Pakka.Compress.ToolResultHeadLines != nil {
		headLines = *s.Pakka.Compress.ToolResultHeadLines
	}
	if s.Pakka.Compress.ToolResultTailLines != nil {
		tailLines = *s.Pakka.Compress.ToolResultTailLines
	}
	return
}

// isToolResultEnabled returns whether tool result compression is enabled.
// Defaults to true if not explicitly set.
//
// Purpose: Check if tool result truncation should run.
// Errors: Never errors.
func isToolResultEnabled() bool {
	s := loadSettings()
	if s.Pakka.Compress.ToolResult == nil {
		return true
	}
	return *s.Pakka.Compress.ToolResult
}

// isInputEnabled returns whether input-file compression is enabled.
// Defaults to true if not explicitly set.
//
// Purpose: Per-vector gate for SessionStart auto-compression of context files.
// Errors: Never errors.
func isInputEnabled() bool {
	s := loadSettings()
	if s.Pakka.Compress.Input == nil {
		return true
	}
	return *s.Pakka.Compress.Input
}

// runCompressToolResult truncates large tool results from PostToolUse events.
// Edit/Write tools and error exits are passed through unchanged.
//
// Purpose: Reduce context consumption from large Read/Grep/Bash outputs.
// Errors: Emits nothing (pass through) on any error condition.
func runCompressToolResult(event *hookevent.Event) {
	if !isToolResultEnabled() {
		return
	}

	// Never truncate Edit/Write — model needs full confirmation
	if event.ToolName == "Edit" || event.ToolName == "Write" {
		return
	}

	// Extract tool response text
	var response string
	if json.Unmarshal(event.ToolResponse, &response) != nil {
		response = string(event.ToolResponse)
	}

	if response == "" {
		return
	}

	// Check for error exit code in tool input (Bash commands)
	// PostToolUse events for Bash include exit_code in the event
	if event.ToolName == "Bash" {
		// Check if response indicates an error (exit code != 0)
		// Claude Code sets tool_response to error text on non-zero exit
		// We check for exit_code field in the event JSON
		// Conservative: check if event has an exit_code field via ToolInput
		var bashInput struct {
			ExitCode *int `json:"exit_code"`
		}
		_ = json.Unmarshal(event.ToolInput, &bashInput)
		if bashInput.ExitCode != nil && *bashInput.ExitCode != 0 {
			return // error output — pass through full
		}
	}

	maxBytes, headLines, tailLines := loadToolResultConfig()

	// Under threshold — pass through
	if len(response) <= maxBytes {
		return
	}

	// Truncate: keep first headLines + last tailLines
	lines := strings.Split(response, "\n")
	totalLines := len(lines)

	if totalLines <= headLines+tailLines {
		// Not enough lines to truncate meaningfully
		return
	}

	truncatedLines := totalLines - headLines - tailLines

	// Compute byte size of head and tail slices we keep, so the message
	// reports bytes actually removed (len(response) - kept), not total size.
	headBytes := 0
	for i := 0; i < headLines && i < totalLines; i++ {
		headBytes += len(lines[i]) + 1 // +1 for newline separator
	}
	tailBytes := 0
	for i := totalLines - tailLines; i < totalLines; i++ {
		tailBytes += len(lines[i]) + 1
	}
	truncatedBytes := len(response) - headBytes - tailBytes
	if truncatedBytes < 0 {
		truncatedBytes = 0
	}

	var b strings.Builder
	for i := 0; i < headLines && i < totalLines; i++ {
		b.WriteString(lines[i])
		b.WriteByte('\n')
	}
	b.WriteString(fmt.Sprintf("\n[pakka: truncated %d lines, %s. Use offset/limit to read specific ranges.]\n\n",
		truncatedLines, compress.FmtSize(truncatedBytes)))
	for i := totalLines - tailLines; i < totalLines; i++ {
		b.WriteString(lines[i])
		if i < totalLines-1 {
			b.WriteByte('\n')
		}
	}

	truncated := b.String()

	// Emit truncated result
	out := map[string]interface{}{
		"hookSpecificOutput": map[string]interface{}{
			"hookEventName": "PostToolUse",
			"toolResult":    truncated,
		},
	}
	_ = json.NewEncoder(os.Stdout).Encode(out)

	// Log savings to meter
	saved := int64(len(response) - len(truncated))
	if saved > 0 {
		_ = meter.WriteSavings(event.SessionID, meter.RepoKey(event.CWD), saved)
	}

	debugLogf("compress tool-result: %s %s → %s (%d lines truncated)",
		event.ToolName, compress.FmtSize(len(response)), compress.FmtSize(len(truncated)), truncatedLines)
}

// --- compress --phase=subagent-return (Pass 4.1) ---

// isSubagentReturnEnabled returns whether subagent return compression is enabled.
// Defaults to true if not explicitly set.
//
// Purpose: Check if subagent return compression should run.
// Errors: Never errors.
func isSubagentReturnEnabled() bool {
	s := loadSettings()
	if s.Pakka.Compress.SubagentReturn == nil {
		return true
	}
	return *s.Pakka.Compress.SubagentReturn
}

// runCompressSubagentReturn applies structural + linguistic compression to
// subagent return text. Skips if text is 1KB or smaller.
//
// Purpose: Compress verbose subagent returns before they enter parent context.
// Errors: Emits nothing (pass through) if compression not worthwhile.
func runCompressSubagentReturn(event *hookevent.Event) {
	if !isSubagentReturnEnabled() {
		return
	}

	// Extract subagent return text
	var input string
	if json.Unmarshal(event.ToolResponse, &input) != nil {
		input = string(event.ToolResponse)
	}

	if input == "" {
		return
	}

	// Skip if <= 1KB — not worth compressing
	if len(input) <= 1024 {
		return
	}

	// Subagent-return always uses strict structural+linguistic compression.
	// The audit/strict engine-mode toggle was removed in Pass 4.1.1; the
	// per-vector boolean (`compress.subagentReturn`) gates the whole call.
	result := compress.Run(input, compress.ModeStrict)

	// Only emit if compression actually saved something
	if result.CompressedSize >= result.OriginalSize {
		return
	}

	fmt.Fprint(os.Stdout, result.Output)
	fmt.Fprintf(os.Stderr, "pakka: subagent-return %s\n", compress.FormatRatio(result))

	saved := int64(result.OriginalSize - result.CompressedSize)
	if saved > 0 {
		_ = meter.WriteSavings(event.SessionID, meter.RepoKey(event.CWD), saved)
	}
}
