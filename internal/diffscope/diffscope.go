// Package diffscope computes the set of (file, line) pairs that are added
// or modified in a staged diff, and filters review findings to that scope.
//
// Reviewer/security agents may emit findings on lines they observe but did
// not change (pre-existing code). Without scoping, a markdown-only commit
// could be blocked by a Go finding on an unstaged file. diffscope is the
// safety net: regardless of agent prompting, findings outside the changed
// line set are dropped.
//
// Input format: unified diff with zero context (`git diff --unified=0` or
// `git diff --cached --unified=0`). Hunk headers carry new-file line ranges.
package diffscope

import (
	"strconv"
	"strings"
)

// Finding is the minimal shape needed for scope filtering. It mirrors the
// fields used by /pakka:review and the commit gate. Other fields on the
// caller's Finding type are preserved by Filter (which is generic over a
// caller-supplied accessor).
type Finding interface {
	GetFile() string
	GetLine() int
}

// Scope maps file path -> set of line numbers (1-indexed, post-image) that
// are added or modified by the diff. A nil/empty Scope means "no changes":
// every finding is out-of-scope and will be filtered out.
type Scope map[string]map[int]bool

// Has reports whether (file, line) is in the changed set.
func (s Scope) Has(file string, line int) bool {
	if s == nil {
		return false
	}
	lines, ok := s[file]
	if !ok {
		return false
	}
	return lines[line]
}

// Files returns the sorted list of files referenced by the scope. Useful
// for diagnostics and tests; order is not guaranteed beyond determinism
// within a single call.
func (s Scope) Files() []string {
	out := make([]string, 0, len(s))
	for f := range s {
		out = append(out, f)
	}
	// Simple insertion sort: file count is small in practice.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// ChangedLines parses a unified=0 diff and returns the set of (file, line)
// pairs that were added in the post-image. Context lines and deleted lines
// are excluded by construction (unified=0 has no context, and deletions do
// not contribute to the new-side line range).
//
// Hunk header syntax: "@@ -<old>[,<oldCount>] +<newStart>[,<newCount>] @@".
// When newCount is 0 the hunk represents pure deletion and contributes no
// new-side lines. When newCount is omitted it defaults to 1.
//
// File header: "+++ b/<path>" or "+++ <path>". A "+++ /dev/null" header
// indicates a deletion; we record no lines for that file.
func ChangedLines(diff string) Scope {
	scope := Scope{}
	var currentFile string
	for _, raw := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(raw, "+++ "):
			currentFile = parsePlusFile(raw[4:])
		case strings.HasPrefix(raw, "@@ ") && currentFile != "":
			start, count, ok := parseNewRange(raw)
			if !ok || count == 0 {
				continue
			}
			lines := scope[currentFile]
			if lines == nil {
				lines = map[int]bool{}
				scope[currentFile] = lines
			}
			for i := 0; i < count; i++ {
				lines[start+i] = true
			}
		}
	}
	return scope
}

// parsePlusFile extracts the post-image file path from a "+++" header.
// Returns "" for /dev/null (deletion).
func parsePlusFile(s string) string {
	// Strip a tab-delimited timestamp suffix if present (rare with git).
	if idx := strings.IndexByte(s, '\t'); idx >= 0 {
		s = s[:idx]
	}
	s = strings.TrimSpace(s)
	if s == "/dev/null" {
		return ""
	}
	if strings.HasPrefix(s, "b/") {
		return s[2:]
	}
	return s
}

// parseNewRange extracts (start, count) from a hunk header's new-side range.
// Examples:
//
//	"@@ -2 +2 @@"          -> (2, 1)
//	"@@ -5,0 +6 @@"        -> (6, 1)
//	"@@ -1,3 +1,5 @@"      -> (1, 5)
//	"@@ -1,2 +0,0 @@"      -> (0, 0)  (pure deletion)
func parseNewRange(header string) (int, int, bool) {
	plus := strings.Index(header, "+")
	if plus < 0 {
		return 0, 0, false
	}
	rest := header[plus+1:]
	end := strings.Index(rest, " ")
	if end < 0 {
		return 0, 0, false
	}
	spec := rest[:end]
	startStr := spec
	countStr := "1"
	if comma := strings.IndexByte(spec, ','); comma >= 0 {
		startStr = spec[:comma]
		countStr = spec[comma+1:]
	}
	start, err := strconv.Atoi(startStr)
	if err != nil {
		return 0, 0, false
	}
	count, err := strconv.Atoi(countStr)
	if err != nil {
		return 0, 0, false
	}
	return start, count, true
}

// Filter drops findings whose (file, line) is not in scope. Order of the
// returned slice matches the input. Findings with empty file or non-positive
// line are always dropped (they cannot be scoped).
//
// The caller is responsible for preserving the unfiltered findings in the
// audit log before calling Filter — Filter is intentionally lossy.
func Filter[F Finding](findings []F, scope Scope) []F {
	out := make([]F, 0, len(findings))
	for _, f := range findings {
		file := f.GetFile()
		line := f.GetLine()
		if file == "" || line <= 0 {
			continue
		}
		if scope.Has(file, line) {
			out = append(out, f)
		}
	}
	return out
}
