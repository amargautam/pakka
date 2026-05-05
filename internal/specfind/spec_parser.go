package specfind

import (
	"bufio"
	"os"
	"regexp"
	"strings"
)

// reDatePrefix matches a leading YYYY-MM-DD- date prefix on a spec filename.
var reDatePrefix = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}-`)

// stemFromFilename strips the optional YYYY-MM-DD- prefix and the .md suffix
// from a spec filename, returning the bare slug used for name matching.
//
// Example: "2026-05-05-spec-anchored-review.md" → "spec-anchored-review"
func stemFromFilename(name string) string {
	stem := strings.TrimSuffix(name, ".md")
	stem = reDatePrefix.ReplaceAllString(stem, "")
	return strings.ToLower(stem)
}

// SpecSections holds the extracted sections of a spec file used to build
// the bounded LLM payload.
type SpecSections struct {
	Filename           string
	Heading            string
	AcceptanceCriteria string
	OutOfScope         string
}

// ParseSpec extracts the first H1 heading, the "Acceptance criteria" section,
// and the "Out of scope" section from a spec markdown file.
//
// It is tolerant: missing sections are returned as empty strings.
func ParseSpec(path string) (SpecSections, error) {
	f, err := os.Open(path)
	if err != nil {
		return SpecSections{}, err
	}
	defer f.Close()

	sections := SpecSections{Filename: strings.TrimPrefix(path, "/")}
	// Use base filename only.
	parts := strings.Split(path, "/")
	sections.Filename = parts[len(parts)-1]

	type state int
	const (
		stateNone state = iota
		stateAC
		stateOOS
	)

	current := stateNone
	var acLines, oosLines []string

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()

		// First H1 heading.
		if sections.Heading == "" && strings.HasPrefix(line, "# ") {
			sections.Heading = strings.TrimPrefix(line, "# ")
			current = stateNone
			continue
		}

		// Section transitions on H2.
		if strings.HasPrefix(line, "## ") {
			heading := strings.ToLower(strings.TrimPrefix(line, "## "))
			switch {
			case strings.Contains(heading, "acceptance criteria"):
				current = stateAC
			case strings.Contains(heading, "out of scope"):
				current = stateOOS
			default:
				current = stateNone
			}
			continue
		}

		switch current {
		case stateAC:
			acLines = append(acLines, line)
		case stateOOS:
			oosLines = append(oosLines, line)
		}
	}

	sections.AcceptanceCriteria = strings.TrimSpace(strings.Join(acLines, "\n"))
	sections.OutOfScope = strings.TrimSpace(strings.Join(oosLines, "\n"))
	return sections, scanner.Err()
}
