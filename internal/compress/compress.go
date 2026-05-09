// Package compress provides text compression for Claude Code context.
//
// Modes:
//   - ModeStrict (default): deterministic structural + linguistic rules.
//   - ModeAudit: passthrough, no compression.
//   - ModeSemantic: LLM-rewrite via the semantic subpackage. Default
//     deterministic mode never calls the LLM. Semantic mode is opt-in per call
//     and runs a deterministic Validator gate on every rewrite — see
//     ./semantic for details.
package compress

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/amargautam/pakka/internal/compress/semantic"
)

// Mode selects the compression level.
type Mode string

const (
	ModeStrict   Mode = "strict"
	ModeAudit    Mode = "audit"
	ModeSemantic Mode = "semantic"
)

// Result holds the compression output and metrics.
type Result struct {
	Output         string
	OriginalSize   int
	CompressedSize int
	Ratio          float64
}

// ParseMode converts a string to a Mode, defaulting to ModeStrict.
//
// Purpose: Safe mode parsing for CLI flag values.
// Errors: Never errors; unknown strings map to ModeStrict.
func ParseMode(s string) Mode {
	switch s {
	case "audit":
		return ModeAudit
	case "semantic":
		return ModeSemantic
	default:
		return ModeStrict
	}
}

// SemanticOptions controls a single semantic rewrite invocation. The fields
// are wired into the runner in semantic mode; in any other mode they are
// ignored.
type SemanticOptions struct {
	// Rewriter is the underlying LLM client. Required when calling
	// RunSemantic; production code passes a *semantic.AnthropicClient and
	// tests pass a stub. A nil Rewriter causes RunSemantic to fall back to
	// deterministic strict mode (callers should log this transition).
	Rewriter semantic.Rewriter
	// Level controls prompt aggressiveness. Defaults to LevelStrict when zero.
	Level semantic.Level
	// Context is plumbed through to the rewriter; defaults to context.Background().
	Context context.Context
}

// Run compresses input text using the deterministic engine.
//
// Purpose: Reduce context size for CLAUDE.md, skill bodies, and subagent
// returns. For semantic mode use RunSemantic — Run never calls an LLM.
// Errors: Never returns an error; always returns a valid Result.
func Run(input string, mode Mode) *Result {
	orig := len(input)
	if mode == ModeAudit || orig == 0 {
		return &Result{Output: input, OriginalSize: orig, CompressedSize: orig, Ratio: 0}
	}

	// ModeSemantic without a Rewriter falls through to strict-deterministic
	// here so existing callers that have not migrated keep working safely.
	// Callers that want the semantic rewrite path must use RunSemantic.
	output := apply(input, mode)
	if mode == ModeStrict || mode == ModeSemantic {
		output = applyLinguistic(output)
	}
	comp := len(output)
	var ratio float64
	if orig > 0 {
		ratio = 100.0 * float64(orig-comp) / float64(orig)
	}
	return &Result{Output: output, OriginalSize: orig, CompressedSize: comp, Ratio: ratio}
}

// RunSemantic compresses input via the LLM rewrite path with validator gate
// and cherry-pick retry. Returns a deterministic strict result when opts is
// zero or opts.Rewriter is nil — never silently degrades to no-op.
//
// Purpose: Single entry point for semantic compression with safety net.
// Errors: Returns the validator's *semantic.FailedError if retries exhaust;
// the returned Result.Output carries the ORIGINAL input unchanged in that
// case so callers can ship it without corruption risk.
func RunSemantic(input string, opts SemanticOptions) (*Result, error) {
	orig := len(input)
	if orig == 0 {
		return &Result{Output: input, OriginalSize: 0, CompressedSize: 0, Ratio: 0}, nil
	}
	// Fallback: no rewriter wired. Use deterministic strict so callers always
	// get a non-nil Result and a labeled ratio.
	if opts.Rewriter == nil {
		return Run(input, ModeStrict), nil
	}
	level := opts.Level
	if level == "" {
		// LevelUltra is the brand default — see memory/DECISIONS.md.
		// Callers that want a softer tier must pass Level explicitly.
		level = semantic.LevelUltra
	}
	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}

	out, err := semantic.RunSemantic(ctx, opts.Rewriter, input, level)
	comp := len(out)
	var ratio float64
	if orig > 0 {
		ratio = 100.0 * float64(orig-comp) / float64(orig)
	}
	res := &Result{Output: out, OriginalSize: orig, CompressedSize: comp, Ratio: ratio}
	return res, err
}

// FormatRatio returns a human-readable compression summary.
//
// Purpose: Annotation line for compressed output.
// Errors: None.
func FormatRatio(r *Result) string {
	return fmt.Sprintf("compressed %.1f%% · %s → %s", r.Ratio, FmtSize(r.OriginalSize), FmtSize(r.CompressedSize))
}

func apply(input string, mode Mode) string {
	lines := strings.Split(input, "\n")
	out := make([]string, 0, len(lines))

	inCode := false
	var lastHeading string
	blanks := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Code fence toggle
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			if inCode {
				inCode = false
				out = append(out, line)
				blanks = 0
				continue
			}
			inCode = true
			// Strict mode strips the language tag to save tokens; other modes
			// preserve it verbatim (language tag is load-bearing for tooling).
			if mode == ModeStrict {
				out = append(out, trimmed[:3])
			} else {
				out = append(out, strings.TrimRight(trimmed, " \t"))
			}
			blanks = 0
			continue
		}

		// Inside code block: preserve verbatim
		if inCode {
			out = append(out, line)
			blanks = 0
			continue
		}

		// Critical markers: always preserve
		if hasCriticalMarker(trimmed) {
			blanks = 0
			out = append(out, line)
			continue
		}

		// Blank lines
		if trimmed == "" {
			blanks++
			if mode == ModeStrict {
				continue // strict: strip all blank lines
			}
			if blanks <= 1 {
				out = append(out, "")
			}
			continue
		}
		blanks = 0

		// Consecutive heading deduplication (outside code blocks).
		// Only drops a heading if it immediately follows the identical heading —
		// preserves repeated headings in different sections.
		if strings.HasPrefix(trimmed, "#") {
			if strings.ToLower(trimmed) == lastHeading {
				continue
			}
			lastHeading = strings.ToLower(trimmed)
		} else {
			lastHeading = "" // reset on non-heading content so only truly consecutive duplicates are dropped
		}

		// Strict: collapse inline whitespace, trim trailing spaces
		if mode == ModeStrict {
			line = multiSpace.ReplaceAllString(strings.TrimRight(line, " \t"), " ")
		}

		out = append(out, line)
	}

	result := strings.Join(out, "\n")
	result = strings.TrimRight(result, " \t\n")
	if len(result) > 0 {
		result += "\n"
	}
	return result
}

var criticalRe = regexp.MustCompile(`(?i)\b(TODO|FIXME|SECURITY|HACK|BUG|XXX)\b`)

func hasCriticalMarker(s string) bool {
	return criticalRe.MatchString(s)
}

var multiSpace = regexp.MustCompile(`[ \t]{2,}`)

// FmtSize returns a human-readable size string (e.g. "4.2k").
func FmtSize(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}
