// Package main provides the pakka-core CLI.
//
// pakka-core is the single binary invoked by all pakka hooks and skills.
// Subcommands are added incrementally across build passes (see DESIGN.md §10):
//
//	Pass 1: status-line, audit
//	Pass 2: compress, meter
//	Pass 3: guard, install-git-hook
//	Pass 3.1: commit-gate, help
//	Pass 4: stack-detect, stack-gate, eval
//	Pass 5: report
//	Pass 5b: bench
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/amargautam/pakka/internal/audit"
	"github.com/amargautam/pakka/internal/bench"
	"github.com/amargautam/pakka/internal/commitgate"
	"github.com/amargautam/pakka/internal/compress"
	"github.com/amargautam/pakka/internal/diffscope"
	evalPkg "github.com/amargautam/pakka/internal/eval"
	"github.com/amargautam/pakka/internal/guard"
	"github.com/amargautam/pakka/internal/hookevent"
	"github.com/amargautam/pakka/internal/meter"
	"github.com/amargautam/pakka/internal/report"
	"github.com/amargautam/pakka/internal/stackdetect"
	"github.com/amargautam/pakka/internal/stackgate"
	"github.com/amargautam/pakka/internal/statusline"
)

const version = "0.1.0-dev"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "pakka-core %s — no subcommand\n", version)
		os.Exit(2)
	}

	switch os.Args[1] {
	case "status-line":
		runStatusLine()
	case "audit":
		runAudit()
	case "compress":
		runCompress()
	case "meter":
		runMeter()
	case "guard":
		runGuard()
	case "commit-gate":
		runCommitGate()
	case "help":
		runHelp()
	case "install-git-hook":
		runInstallGitHook()
	case "stack-detect":
		runStackDetect()
	case "stack-gate":
		runStackGate()
	case "eval":
		runEval()
	case "report":
		runReport()
	case "bench":
		runBench()
	case "output-rules":
		runOutputRules()
	case "output-reinforce":
		runOutputReinforce()
	default:
		fmt.Fprintf(os.Stderr, "pakka-core %s — unknown subcommand %q\n", version, os.Args[1])
		os.Exit(2)
	}
}

func runStatusLine() {
	event, _ := hookevent.Parse(os.Stdin)
	mode := loadCompressMode()
	if err := statusline.Run(event, os.Stdout, mode); err != nil {
		fmt.Fprintf(os.Stderr, "pakka: status-line: %v\n", err)
		os.Exit(1)
	}
}

// compressModeRe validates that compress mode contains only lowercase alpha.
var compressModeRe = regexp.MustCompile(`^[a-z]+$`)

func loadCompressMode() string {
	root := pluginRoot()
	data, err := os.ReadFile(filepath.Join(root, "settings.json"))
	if err != nil {
		return "strict"
	}
	var s settingsJSON
	if json.Unmarshal(data, &s) != nil {
		return "strict"
	}
	m := s.Pakka.Compress.Mode
	if !compressModeRe.MatchString(m) {
		return "strict"
	}
	switch m {
	case "strict", "audit":
		return m
	default:
		return "strict"
	}
}

func runAudit() {
	phase := "tool-post"
	for _, a := range os.Args[2:] {
		if strings.HasPrefix(a, "--phase=") {
			phase = strings.TrimPrefix(a, "--phase=")
		}
	}
	event, _ := hookevent.Parse(os.Stdin)
	if err := audit.Run(event, phase); err != nil {
		fmt.Fprintf(os.Stderr, "pakka: audit: %v\n", err)
		os.Exit(1)
	}
}

func runCompress() {
	mode := "strict"
	phase := ""
	for _, a := range os.Args[2:] {
		if strings.HasPrefix(a, "--mode=") {
			mode = strings.TrimPrefix(a, "--mode=")
		}
		if strings.HasPrefix(a, "--phase=") {
			phase = strings.TrimPrefix(a, "--phase=")
		}
	}

	var input string
	var sessionID string

	if phase != "" {
		// Hook invocation: parse event JSON from stdin
		event, _ := hookevent.Parse(os.Stdin)
		sessionID = event.SessionID
		switch phase {
		case "tool-result":
			runCompressToolResult(event)
			return
		case "subagent-return":
			runCompressSubagentReturn(event)
			return
		case "session-start":
			// Auto-compress CLAUDE.md, DESIGN.md, BUILD.md in CWD + one level deep.
			cwd := event.CWD
			if cwd == "" {
				cwd, _ = os.Getwd()
			}
			debugLogf("compress cwd=%s event.cwd=%s", cwd, event.CWD)
			// If --mode not provided, fall back to settings.json
			if mode == "strict" {
				if cfgMode := loadCompressMode(); cfgMode != "" {
					mode = cfgMode
				}
			}
			autoCompressContextFiles(cwd, mode, sessionID)
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
	result := compress.Run(input, m)

	fmt.Fprint(os.Stdout, result.Output)
	fmt.Fprintf(os.Stderr, "pakka: %s\n", compress.FormatRatio(result))

	saved := int64(result.OriginalSize - result.CompressedSize)
	if saved > 0 {
		cwd, _ := os.Getwd()
		_ = meter.WriteSavings(sessionID, meter.RepoKey(cwd), saved)
	}
}

// debugLogf appends a timestamped line to ~/.pakka/debug.log.
// Failures are silently ignored — debug logging must never break the hook.
func debugLogf(format string, args ...interface{}) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	dir := filepath.Join(home, ".pakka")
	_ = os.MkdirAll(dir, 0755)
	f, err := os.OpenFile(filepath.Join(dir, "debug.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(f, "%s %s\n", time.Now().UTC().Format(time.RFC3339), msg)
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
	if len(paths) == 0 && dir != "/" {
		parent := filepath.Dir(dir)
		if parent != dir {
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
	maxBytes = 10240  // 10KB default
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
		var rawEvent map[string]json.RawMessage
		// Re-check: the event struct may have extra fields we don't capture.
		// For Bash, the response on error typically starts with error indicators.
		// Conservative: check if event has an exit_code field via ToolInput
		var bashInput struct {
			ExitCode *int `json:"exit_code"`
		}
		_ = json.Unmarshal(event.ToolInput, &bashInput)
		if bashInput.ExitCode != nil && *bashInput.ExitCode != 0 {
			return // error output — pass through full
		}
		_ = rawEvent // suppress unused
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
	truncatedBytes := len(response) // approximate

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

	m := compress.ParseMode(loadCompressMode())
	result := compress.Run(input, m)

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

func runMeter() {
	event, _ := hookevent.Parse(os.Stdin)
	if err := meter.Run(event); err != nil {
		fmt.Fprintf(os.Stderr, "pakka: meter: %v\n", err)
		os.Exit(1)
	}
}

func runGuard() {
	event, _ := hookevent.Parse(os.Stdin)
	result := guard.Run(event)
	if !result.Allowed {
		_ = audit.RunBlock(event, result.Reason)
		fmt.Fprintf(os.Stderr, "pakka guard: %s\n", result.Reason)
		os.Exit(2)
	}
}

// --- stack-detect (Pass 4) ---

func runStackDetect() {
	// Try to get CWD from event JSON on stdin, fall back to os.Getwd().
	cwd := ""
	event, _ := hookevent.Parse(os.Stdin)
	if event.CWD != "" {
		cwd = event.CWD
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	result := stackdetect.Detect(cwd)
	_ = json.NewEncoder(os.Stdout).Encode(result)
}

// --- stack-gate (Pass 4) ---

func runStackGate() {
	event, _ := hookevent.Parse(os.Stdin)

	// Determine project directory
	cwd := event.CWD
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	// Load .pakka/stack.json — if missing, exit 0 silently
	cfg := stackgate.LoadConfig(cwd)
	if cfg == nil {
		return
	}

	result := stackgate.Run(event, cfg)
	if !result.Passed {
		fmt.Fprint(os.Stderr, result.Output)
		os.Exit(2)
	}
}

// --- eval (Pass 4) ---

func runEval() {
	maxLayer := 3 // default: all layers
	var targets []string
	for _, a := range os.Args[2:] {
		if strings.HasPrefix(a, "--layer=") {
			v := strings.TrimPrefix(a, "--layer=")
			n, err := strconv.Atoi(v)
			if err == nil {
				maxLayer = n
			}
		} else {
			targets = append(targets, a)
		}
	}

	// Auto-discover targets if none provided.
	if len(targets) == 0 {
		root := pluginRoot()
		targets = discoverEvalTargets(root)
	}

	if len(targets) == 0 {
		fmt.Fprintf(os.Stderr, "pakka: eval: no target files found\n")
		os.Exit(2)
	}

	result := evalPkg.Run(targets, maxLayer)

	// Print layer results as JSON lines to stderr.
	for _, lr := range result.Layers {
		data, _ := json.Marshal(lr)
		fmt.Fprintln(os.Stderr, string(data))
	}

	// Write full results to .pakka/eval/<ts>.json.
	ts := time.Now().Format("20060102-150405")
	evalDir := ".pakka/eval"
	_ = os.MkdirAll(evalDir, 0755)
	data, _ := json.MarshalIndent(result, "", "  ")
	_ = os.WriteFile(filepath.Join(evalDir, ts+".json"), data, 0644)

	if !result.Passed {
		os.Exit(2)
	}
}

// discoverEvalTargets finds skill, agent, and command files under the
// plugin root.
//
// Purpose: Auto-discover targets when none are provided on the command line.
// The eval package classifies each path and applies the right schema.
// Errors: Silently skips unreadable directories; returns nil if none found.
func discoverEvalTargets(root string) []string {
	var targets []string

	// skills/*/SKILL.md
	skillsDir := filepath.Join(root, "skills")
	if entries, err := os.ReadDir(skillsDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			p := filepath.Join(skillsDir, e.Name(), "SKILL.md")
			if _, err := os.Stat(p); err == nil {
				targets = append(targets, p)
			}
		}
	}

	// agents/*.md
	agentsDir := filepath.Join(root, "agents")
	if entries, err := os.ReadDir(agentsDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if strings.HasSuffix(e.Name(), ".md") {
				targets = append(targets, filepath.Join(agentsDir, e.Name()))
			}
		}
	}

	// commands/*.md
	commandsDir := filepath.Join(root, "commands")
	if entries, err := os.ReadDir(commandsDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if strings.HasSuffix(e.Name(), ".md") {
				targets = append(targets, filepath.Join(commandsDir, e.Name()))
			}
		}
	}

	return targets
}

// --- report (Pass 5) ---

func runReport() {
	format := "md"
	for _, a := range os.Args[2:] {
		if strings.HasPrefix(a, "--format=") {
			format = strings.TrimPrefix(a, "--format=")
		}
	}
	_ = format // only md for now

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "pakka: report: %v\n", err)
		os.Exit(1)
	}
	meterDir := filepath.Join(home, ".pakka", "meter")
	auditDir := filepath.Join(home, ".pakka", "audit")

	stats, err := report.Gather(meterDir, auditDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pakka: report: %v\n", err)
		os.Exit(1)
	}

	fmt.Print(report.FormatMarkdown(stats, version))
}

// --- bench (Pass 5b) ---

func runBench() {
	opts := bench.Options{
		Mode:      "both",
		ClaudeBin: "claude",
		Timeout:   180 * time.Second,
	}
	for _, a := range os.Args[2:] {
		switch {
		case strings.HasPrefix(a, "--corpus="):
			opts.CorpusPath = strings.TrimPrefix(a, "--corpus=")
		case strings.HasPrefix(a, "--out="):
			opts.OutPath = strings.TrimPrefix(a, "--out=")
		case strings.HasPrefix(a, "--limit="):
			n, err := strconv.Atoi(strings.TrimPrefix(a, "--limit="))
			if err == nil {
				opts.Limit = n
			}
		case strings.HasPrefix(a, "--mode="):
			opts.Mode = strings.TrimPrefix(a, "--mode=")
		case strings.HasPrefix(a, "--claude-bin="):
			opts.ClaudeBin = strings.TrimPrefix(a, "--claude-bin=")
		case strings.HasPrefix(a, "--timeout="):
			n, err := strconv.Atoi(strings.TrimPrefix(a, "--timeout="))
			if err == nil {
				opts.Timeout = time.Duration(n) * time.Second
			}
		case a == "--verbose":
			opts.Verbose = true
		}
	}

	if opts.CorpusPath == "" || opts.OutPath == "" {
		fmt.Fprintf(os.Stderr, "pakka: bench: --corpus and --out are required\n")
		os.Exit(2)
	}

	if err := bench.Run(opts); err != nil {
		fmt.Fprintf(os.Stderr, "pakka: bench: %v\n", err)
		os.Exit(1)
	}
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
			Mode                string `json:"mode"`
			Output              *bool  `json:"output"`
			OutputLevel         string `json:"outputLevel"`
			ToolResult          *bool  `json:"toolResult"`
			ToolResultMaxBytes  *int   `json:"toolResultMaxBytes"`
			ToolResultHeadLines *int   `json:"toolResultHeadLines"`
			ToolResultTailLines *int   `json:"toolResultTailLines"`
			SubagentReturn      *bool  `json:"subagentReturn"`
		} `json:"compress"`
		Display struct {
			StatusLine *bool `json:"statusLine"`
		} `json:"display"`
	} `json:"pakka"`
}

func runCommitGate() {
	event, _ := hookevent.Parse(os.Stdin)

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
		mode := loadCompressMode()
		summary := statusline.Summary(event, mode)
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
		out := map[string]interface{}{
			"tool_input": map[string]string{"command": d.Command},
		}
		_ = json.NewEncoder(os.Stdout).Encode(out)
	}
}

func pluginRoot() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	// Binary is at <root>/bin/pakka-core-<os>-<arch>; root is two levels up.
	return filepath.Dir(filepath.Dir(exe))
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

func gatherReviewState(cfg *commitgate.Config) *commitgate.State {
	state := &commitgate.State{}

	// Diff size via git
	out, err := exec.Command("git", "diff", "--cached").Output()
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
func scopeFromStagedDiff() diffscope.Scope {
	out, err := exec.Command("git", "diff", "--cached", "--unified=0").Output()
	if err != nil {
		return diffscope.Scope{}
	}
	return diffscope.ChangedLines(string(out))
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
func loadLatestErrors(threshold int, scope diffscope.Scope) []commitgate.Finding {
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
	return diffscope.Filter(findings, scope)
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

// --- output-rules (Pass 4.1) ---

// loadSettings reads and parses settings.json from the plugin root.
//
// Purpose: Shared config loader for output compression subcommands.
// Errors: Returns zero-value settingsJSON on any read/parse failure.
func loadSettings() settingsJSON {
	root := pluginRoot()
	data, err := os.ReadFile(filepath.Join(root, "settings.json"))
	if err != nil {
		return settingsJSON{}
	}
	var s settingsJSON
	_ = json.Unmarshal(data, &s)
	return s
}

// loadOutputLevel returns the configured output compression level.
// Falls back to "strict" if not set or invalid.
//
// Purpose: Determine output compression intensity for rules and reinforcement.
// Errors: Never errors; invalid values map to "strict".
func loadOutputLevel() string {
	s := loadSettings()
	switch s.Pakka.Compress.OutputLevel {
	case "lite", "strict", "ultra":
		return s.Pakka.Compress.OutputLevel
	default:
		return "strict"
	}
}

// isOutputEnabled returns whether output compression is enabled.
// Defaults to true if not explicitly set to false.
//
// Purpose: Check if output compression subcommands should emit anything.
// Errors: Never errors.
func isOutputEnabled() bool {
	s := loadSettings()
	if s.Pakka.Compress.Output == nil {
		return true
	}
	return *s.Pakka.Compress.Output
}

// outputCompressRulesetFallback is emitted when rules/output-compress.md is missing.
const outputCompressRulesetFallback = `PAKKA OUTPUT COMPRESSION ACTIVE — level: strict

## Persistence
Active every response. No revert after many turns. No filler drift.
Still active if unsure. Off only: user says "pakka verbose" or "normal mode".
Default: strict. Switch: /pakka:compress lite|strict|ultra

## Rules
Drop: articles (a/an/the), filler (just/really/basically/actually/simply),
pleasantries (sure/certainly/of course/happy to), hedging (I think/maybe/perhaps).
Fragments OK. Short synonyms (big not extensive, fix not "implement a solution for").
Technical terms exact. Code blocks unchanged. Errors quoted exact.
Pattern: [thing] [action] [reason]. [next step].

Not: "Sure! I'd be happy to help you with that. The issue you're experiencing is..."
Yes: "Bug in auth middleware. Token expiry uses < not <=. Fix:"

## Intensity
| Level | Rules |
|-------|-------|
| lite | No filler/hedging. Keep articles + full sentences. Professional tight. |
| strict | Drop articles, fragments OK, short synonyms. Default. |
| ultra | Abbreviate (DB/auth/config/req/res/fn/impl), strip conjunctions, arrows for causality (X -> Y), one word when one word enough. |

## Auto-Clarity
Drop compression for: security warnings, irreversible action confirmations,
multi-step sequences where fragments risk misread, user asks to clarify.
Resume after clear part done.

## Boundaries
Code/commits/PRs/error messages: write normal. Never compress code output.
`

// runOutputRules reads the output compression ruleset and emits it to stdout.
// Used by SessionStart hook to inject output compression rules into context.
//
// Purpose: Provide output compression rules as additional session context.
// Errors: Falls back to hardcoded ruleset if file not found.
func runOutputRules() {
	if !isOutputEnabled() {
		return
	}

	level := loadOutputLevel()

	// Try to read ruleset from plugin root
	root := os.Getenv("CLAUDE_PLUGIN_ROOT")
	if root == "" {
		root = pluginRoot()
	}
	rulesetPath := filepath.Join(root, "rules", "output-compress.md")

	content, err := os.ReadFile(rulesetPath)
	if err != nil {
		// Fallback to hardcoded ruleset
		content = []byte(outputCompressRulesetFallback)
	}

	// Replace the level in the ruleset header
	out := strings.Replace(string(content), "level: strict", "level: "+level, 1)
	fmt.Fprint(os.Stdout, out)
}

// --- output-reinforce (Pass 4.1) ---

// runOutputReinforce emits a short per-turn reinforcement JSON to stdout.
// Used by UserPromptSubmit hook to prevent drift after many turns.
//
// Purpose: Reinforce output compression rules on every user prompt.
// Errors: None; always succeeds or emits nothing.
func runOutputReinforce() {
	if !isOutputEnabled() {
		return
	}

	level := loadOutputLevel()

	reinforce := map[string]interface{}{
		"hookSpecificOutput": map[string]interface{}{
			"hookEventName":     "UserPromptSubmit",
			"additionalContext": fmt.Sprintf("PAKKA OUTPUT COMPRESSION ACTIVE (%s). Drop articles/filler/pleasantries/hedging. Fragments OK. Code/commits/security: write normal.", level),
		},
	}

	_ = json.NewEncoder(os.Stdout).Encode(reinforce)
}

// --- help (Pass 3.1) ---

func runHelp() {
	root := pluginRoot()

	data, _ := os.ReadFile(filepath.Join(root, "settings.json"))
	var s settingsJSON
	_ = json.Unmarshal(data, &s)

	// Resolve config with defaults
	autoGate := true
	threshold := 80
	compressMode := "strict"
	guardOn := true
	sigOn := true
	coAuthorOn := true

	if s.Pakka.Review.AutoGate != nil {
		autoGate = *s.Pakka.Review.AutoGate
	}
	if s.Pakka.Review.ConfidenceThreshold != nil {
		threshold = *s.Pakka.Review.ConfidenceThreshold
	}
	if s.Pakka.Compress.Mode != "" {
		compressMode = s.Pakka.Compress.Mode
	}
	if s.Pakka.Signature != nil {
		sigOn = *s.Pakka.Signature
	}
	if s.Pakka.CoAuthor != nil {
		coAuthorOn = *s.Pakka.CoAuthor
	}
	_ = guardOn // guard is always on if the hook is registered

	// Find latest session from audit files
	home, _ := os.UserHomeDir()
	auditDir := filepath.Join(home, ".pakka", "audit")
	meterDir := filepath.Join(home, ".pakka", "meter")

	sessionID := "none"
	auditFile := ""
	auditCount := 0
	meterFile := ""
	meterTok := 0

	if entries, err := os.ReadDir(auditDir); err == nil {
		var latestName string
		var latestTime time.Time
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			if info.ModTime().After(latestTime) {
				latestTime = info.ModTime()
				latestName = e.Name()
			}
		}
		if latestName != "" {
			sessionID = strings.TrimSuffix(latestName, ".jsonl")
			auditFile = "~/.pakka/audit/" + latestName
			auditCount = countJSONLEvents(filepath.Join(auditDir, latestName))
		}
	}

	meterPath := filepath.Join(meterDir, sessionID+".jsonl")
	if _, err := os.Stat(meterPath); err == nil {
		meterFile = "~/.pakka/meter/" + sessionID + ".jsonl"
		meterTok = countTokens(meterPath)
	}

	onOff := func(b bool) string {
		if b {
			return "on"
		}
		return "off"
	}

	outputLevel := loadOutputLevel()
	outputOn := isOutputEnabled()

	fmt.Printf("pakka v%s · session %s\n", version, sessionID)
	fmt.Printf("  auto         review-gate: %-3s (threshold %d)  · compress: %s\n", onOff(autoGate), threshold, compressMode)
	fmt.Printf("               guard: %-3s                       · signature: %s\n", onOff(guardOn), onOff(sigOn))
	fmt.Printf("               coAuthor: %-3s                    · output: %s [%s]\n", onOff(coAuthorOn), onOff(outputOn), outputLevel)
	fmt.Printf("  commands     /pakka:review    explicit review of staged diff\n")
	fmt.Printf("               /pakka:compress  switch output level (lite|strict|ultra)\n")
	fmt.Printf("               /pakka:help      this page\n")
	if auditFile != "" {
		fmt.Printf("  audit        %s  · %d events\n", auditFile, auditCount)
	} else {
		fmt.Printf("  audit        (no session)\n")
	}
	if meterFile != "" {
		fmt.Printf("  meter        %s  · %d tok\n", meterFile, meterTok)
	} else {
		fmt.Printf("  meter        (no session)\n")
	}
	fmt.Printf("  attribution  pakka <279024857+pakka-bot@users.noreply.github.com>\n")
	fmt.Printf("  docs         pakka.dev\n")
}

func countJSONLEvents(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	count := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	// Subtract the schema preamble line
	if count > 0 {
		count--
	}
	return count
}

func countTokens(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	total := 0
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry struct {
			TokensUsed int `json:"tokens_used"`
		}
		if json.Unmarshal([]byte(line), &entry) == nil {
			total += entry.TokensUsed
		}
	}
	return total
}

// --- install-git-hook (Pass 3) ---

const prepareCommitMsgHook = `#!/bin/sh
# Installed by pakka — appends Reviewed-by-pakka trailer to human-authored commits.
# Claude Code commits are auto-signed via the PreToolUse commit-gate hook.
COMMIT_MSG_FILE="$1"
TRAILER="Reviewed-by-pakka: v0.1.0"
PASS_FILE=".pakka/reviews/last-pass-ts"
MAX_AGE=300

if [ ! -f "$PASS_FILE" ]; then exit 0; fi
PASS_TS=$(cat "$PASS_FILE" 2>/dev/null)
NOW=$(date +%s)
AGE=$(( NOW - PASS_TS ))
if [ "$AGE" -gt "$MAX_AGE" ]; then exit 0; fi
if grep -qF "$TRAILER" "$COMMIT_MSG_FILE" 2>/dev/null; then exit 0; fi
printf '\n%s\n' "$TRAILER" >> "$COMMIT_MSG_FILE"
`

func runInstallGitHook() {
	// Find git directory
	gitDir := ".git"
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "pakka: not a git repository\n")
		os.Exit(1)
	}

	hookPath := filepath.Join(gitDir, "hooks", "prepare-commit-msg")
	stateFile := filepath.Join(".pakka", "hook-installed")

	// Idempotent: check if already installed
	if _, err := os.Stat(stateFile); err == nil {
		fmt.Fprintf(os.Stderr, "pakka: git hook already installed at %s\n", hookPath)
		return
	}

	// Create hooks directory
	if err := os.MkdirAll(filepath.Join(gitDir, "hooks"), 0755); err != nil {
		fmt.Fprintf(os.Stderr, "pakka: %v\n", err)
		os.Exit(1)
	}

	// Write hook
	if err := os.WriteFile(hookPath, []byte(prepareCommitMsgHook), 0755); err != nil {
		fmt.Fprintf(os.Stderr, "pakka: %v\n", err)
		os.Exit(1)
	}

	// Mark installed
	if err := os.MkdirAll(".pakka", 0755); err == nil {
		_ = os.WriteFile(stateFile, []byte("installed\n"), 0644)
	}

	fmt.Fprintf(os.Stderr, "pakka: installed prepare-commit-msg hook at %s\n", hookPath)
	fmt.Fprintf(os.Stderr, "pakka: optional — for human-authored commits. Claude Code commits are auto-signed via PreToolUse.\n")
}
