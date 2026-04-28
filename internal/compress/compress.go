// Package compress provides deterministic text compression for Claude Code context.
//
// Modes: strict (default), audit (passthrough).
// No LLM calls. All rules are mechanical and reproducible.
package compress

import (
	"fmt"
	"regexp"
	"strings"
)

// Mode selects the compression level.
type Mode string

const (
	ModeStrict Mode = "strict"
	ModeAudit  Mode = "audit"
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
	default:
		return ModeStrict
	}
}

// Run compresses input text deterministically using the given mode.
//
// Purpose: Reduce context size for CLAUDE.md, skill bodies, and subagent returns.
// Errors: Never returns an error; always returns a valid Result.
func Run(input string, mode Mode) *Result {
	orig := len(input)
	if mode == ModeAudit || orig == 0 {
		return &Result{Output: input, OriginalSize: orig, CompressedSize: orig, Ratio: 0}
	}

	output := apply(input, mode)
	if mode == ModeStrict {
		output = applyLinguistic(output)
	}
	comp := len(output)
	var ratio float64
	if orig > 0 {
		ratio = 100.0 * float64(orig-comp) / float64(orig)
	}
	return &Result{Output: output, OriginalSize: orig, CompressedSize: comp, Ratio: ratio}
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
	seen := make(map[string]bool)
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
			// Strip language identifier from opening fence
			out = append(out, trimmed[:3])
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

		// Heading deduplication (outside code blocks)
		if strings.HasPrefix(trimmed, "#") {
			key := strings.ToLower(trimmed)
			if seen[key] {
				continue
			}
			seen[key] = true
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
