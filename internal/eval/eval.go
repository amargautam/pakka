// Package eval implements the 3-layer eval gate for skill and agent files.
//
// Layer 1 performs static checks: frontmatter validation, banned-word
// scanning, Red Flags section enforcement, and line-length limits.
// Layers 2 and 3 are deferred to the skill wrapper (require LLM calls
// and headless claude -p respectively).
//
// Exit codes (when invoked via pakka-core eval): 0 pass, 2 fail.
package eval

import (
	"bufio"
	"os"
	"strings"
)

// LayerResult represents the outcome of one eval layer on one target.
type LayerResult struct {
	Layer   int      `json:"layer"`
	Target  string   `json:"target"`
	Passed  bool     `json:"passed"`
	Score   int      `json:"score,omitempty"`
	Details string   `json:"details"`
	Errors  []string `json:"errors,omitempty"`
}

// Result is the full eval outcome.
type Result struct {
	Layers []LayerResult `json:"layers"`
	Passed bool          `json:"passed"`
}

// bannedWords is the list of words that must not appear in skill/agent body text.
var bannedWords = []string{
	"guarantee",
	"100%",
	"revolutionary",
	"seamless",
	"delightful",
	"unlock",
	"empower",
}

// Run executes eval layers on the given targets.
//
// Purpose: Entry point for the eval subcommand.
// Errors: Returns non-nil Result always; sets Passed=false if any layer fails.
func Run(targets []string, maxLayer int) *Result {
	if maxLayer <= 0 {
		maxLayer = 3
	}

	r := &Result{Passed: true}

	for _, t := range targets {
		if maxLayer >= 1 {
			lr := runLayer1(t)
			r.Layers = append(r.Layers, lr)
			if !lr.Passed {
				r.Passed = false
			}
		}

		if maxLayer >= 2 {
			lr := runLayer2(t)
			r.Layers = append(r.Layers, lr)
		}

		if maxLayer >= 3 {
			lr := runLayer3(t)
			r.Layers = append(r.Layers, lr)
		}
	}

	// Ensure Layers is never nil (marshal as [] not null)
	if r.Layers == nil {
		r.Layers = []LayerResult{}
	}

	return r
}

// runLayer1 performs static checks on a single target file.
//
// Purpose: Validate frontmatter, scan for banned words, require Red Flags
// section, and enforce line-length limits.
// Errors: Failures are reported in LayerResult.Errors, never returned as error.
func runLayer1(target string) LayerResult {
	lr := LayerResult{
		Layer:  1,
		Target: target,
		Passed: true,
	}

	data, err := os.ReadFile(target)
	if err != nil {
		lr.Passed = false
		lr.Errors = append(lr.Errors, "cannot read file: "+err.Error())
		lr.Details = "layer-1: cannot read file"
		return lr
	}

	content := string(data)
	if strings.TrimSpace(content) == "" {
		lr.Passed = false
		lr.Errors = append(lr.Errors, "empty file")
		lr.Details = "layer-1: empty file"
		return lr
	}

	// --- Frontmatter validation ---
	body := checkFrontmatter(content, &lr)

	// --- Banned words (scan body only, not frontmatter) ---
	checkBannedWords(body, &lr)

	// --- Red Flags section ---
	checkRedFlags(content, &lr)

	// --- Line length ---
	checkLineLength(content, &lr)

	// Build details summary
	if lr.Passed {
		lr.Details = "layer-1: all checks passed"
	} else {
		lr.Details = "layer-1: " + strings.Join(lr.Errors, "; ")
	}

	return lr
}

// checkFrontmatter validates YAML frontmatter delimiters and required fields.
// Returns the body text (everything after the closing ---).
//
// Purpose: Ensure skill/agent files have valid frontmatter with required fields.
// Errors: Appends to lr.Errors on failure.
func checkFrontmatter(content string, lr *LayerResult) string {
	lines := strings.SplitAfter(content, "\n")

	// Must start with ---
	if len(lines) == 0 || strings.TrimSpace(strings.TrimRight(lines[0], "\n")) != "---" {
		lr.Passed = false
		lr.Errors = append(lr.Errors, "missing frontmatter")
		return content
	}

	// Find closing ---
	closingIdx := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(strings.TrimRight(lines[i], "\n")) == "---" {
			closingIdx = i
			break
		}
	}

	if closingIdx < 0 {
		lr.Passed = false
		lr.Errors = append(lr.Errors, "missing frontmatter")
		return content
	}

	// Extract frontmatter fields
	fm := strings.Join(lines[1:closingIdx], "")

	hasName := false
	hasDescription := false
	hasTools := false

	scanner := bufio.NewScanner(strings.NewReader(fm))
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "name:") {
			hasName = true
		}
		if strings.HasPrefix(trimmed, "description:") {
			hasDescription = true
		}
		if strings.HasPrefix(trimmed, "tools:") {
			hasTools = true
		}
	}

	if !hasName {
		lr.Passed = false
		lr.Errors = append(lr.Errors, "frontmatter missing name field")
	}
	if !hasDescription {
		lr.Passed = false
		lr.Errors = append(lr.Errors, "frontmatter missing description field")
	}

	// Determine if this is an agent file (has tools: field or filename contains "agent")
	// For agent files, tools: is required. We check it only if tools: is present
	// or if the filename pattern suggests an agent file.
	// For now, just track hasTools — the caller can use target filename to decide.
	_ = hasTools

	// Return body (everything after closing ---)
	if closingIdx+1 < len(lines) {
		return strings.Join(lines[closingIdx+1:], "")
	}
	return ""
}

// checkBannedWords scans the body for banned words (case-insensitive).
//
// Purpose: Enforce brand voice guidelines by rejecting hype words.
// Errors: Appends one entry per banned word found to lr.Errors.
func checkBannedWords(body string, lr *LayerResult) {
	lower := strings.ToLower(body)
	for _, w := range bannedWords {
		if strings.Contains(lower, strings.ToLower(w)) {
			lr.Passed = false
			lr.Errors = append(lr.Errors, "banned word: "+w)
		}
	}
}

// checkRedFlags verifies the file contains a Red Flags section with at least
// one bullet point.
//
// Purpose: Every skill/agent must document its failure modes.
// Errors: Appends "missing Red Flags section" to lr.Errors if absent.
func checkRedFlags(content string, lr *LayerResult) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	foundHeader := false
	hasBullet := false

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if foundHeader {
			// Look for a bullet point (- or *)
			if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
				hasBullet = true
				break
			}
			// If we hit another heading or significant content before a bullet, keep going
			if strings.HasPrefix(trimmed, "#") && !strings.Contains(strings.ToLower(trimmed), "red flags") {
				// Hit another section without finding a bullet under Red Flags
				break
			}
			continue
		}

		// Check for ## Red Flags or ### Red Flags (case-insensitive on "Red Flags")
		if (strings.HasPrefix(trimmed, "## ") || strings.HasPrefix(trimmed, "### ")) &&
			strings.Contains(strings.ToLower(trimmed), "red flags") {
			foundHeader = true
		}
	}

	if !foundHeader || !hasBullet {
		lr.Passed = false
		lr.Errors = append(lr.Errors, "missing Red Flags section")
	}
}

// checkLineLength ensures no line exceeds 200 characters, excluding lines
// inside code blocks (between ``` markers) and lines containing URLs.
//
// Purpose: Enforce readability limits on skill/agent prose.
// Errors: Appends "line too long" to lr.Errors for each violation.
func checkLineLength(content string, lr *LayerResult) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	inCodeBlock := false
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// Track code block boundaries
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inCodeBlock = !inCodeBlock
			continue
		}

		if inCodeBlock {
			continue
		}

		// Skip URL lines
		if strings.Contains(line, "http://") || strings.Contains(line, "https://") {
			continue
		}

		if len(line) > 200 {
			lr.Passed = false
			lr.Errors = append(lr.Errors, "line too long")
			// Report only once per file to avoid noise
			return
		}
	}
}

// runLayer2 returns a pass-through result for Layer 2 (LLM judge).
//
// Purpose: Placeholder for LLM-based evaluation, deferred to skill wrapper.
// Errors: None; always returns passed=true.
func runLayer2(target string) LayerResult {
	return LayerResult{
		Layer:   2,
		Target:  target,
		Passed:  true,
		Score:   0,
		Details: "layer-2: deferred to skill wrapper",
	}
}

// runLayer3 returns a pass-through result for Layer 3 (headless claude -p).
//
// Purpose: Placeholder for headless Claude evaluation, deferred to skill wrapper.
// Errors: None; always returns passed=true.
func runLayer3(target string) LayerResult {
	return LayerResult{
		Layer:   3,
		Target:  target,
		Passed:  true,
		Details: "layer-3: deferred to skill wrapper (requires headless claude -p)",
	}
}
