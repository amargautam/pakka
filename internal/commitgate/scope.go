// Scope, ChangedLines, and Filter moved from internal/diffscope.
// diffscope was only used by the commit-gate flow; collapsing the seam here
// eliminates the cross-package dependency. commitgate.Finding already
// satisfies ScopedFinding via GetFile/GetLine.
package commitgate

import (
	"strconv"
	"strings"
)

// ScopedFinding is the minimal interface for scope filtering. commitgate.Finding
// satisfies it via GetFile/GetLine; callers may also supply their own types.
type ScopedFinding interface {
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

// Files returns the sorted list of files referenced by the scope.
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
// pairs that were added in the post-image.
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
func parsePlusFile(s string) string {
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

// Filter drops findings whose (file, line) is not in scope. Findings with
// empty file or non-positive line are always dropped.
func Filter[F ScopedFinding](findings []F, scope Scope) []F {
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
