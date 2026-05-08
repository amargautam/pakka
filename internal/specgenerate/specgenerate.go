package specgenerate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Options controls spec file generation.
type Options struct {
	Slug     string // required; descriptive kebab name
	Date     string // YYYY-MM-DD; empty = today
	SpecsDir string // default "docs/specs/"
	Content  string // full spec markdown content
}

// Result describes what Generate did.
type Result struct {
	Path  string // path written
	Diff  string // unified diff if file existed; empty for new files
	IsNew bool   // true = created fresh
}

var requiredSections = []string{
	"problem",
	"user stories",
	"module decisions",
	"acceptance criteria",
	"out of scope",
	"open questions",
}

// Generate validates opts, creates SpecsDir if needed, and writes the spec file.
func Generate(opts Options) (Result, error) {
	if opts.Slug == "" {
		return Result{}, fmt.Errorf("slug is required")
	}
	if !validSlug(opts.Slug) {
		return Result{}, fmt.Errorf("invalid slug %q: use lowercase letters, digits, and hyphens only", opts.Slug)
	}

	missing := missingH2Sections(opts.Content)
	if len(missing) > 0 {
		return Result{}, fmt.Errorf("missing required sections: %s", strings.Join(missing, ", "))
	}

	if opts.Date == "" {
		opts.Date = time.Now().Format("2006-01-02")
	}
	if opts.SpecsDir == "" {
		opts.SpecsDir = "docs/specs/"
	}

	if err := os.MkdirAll(opts.SpecsDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("creating specs dir: %w", err)
	}

	target := filepath.Join(opts.SpecsDir, opts.Date+"-"+opts.Slug+".md")

	_, statErr := os.Stat(target)
	if os.IsNotExist(statErr) {
		if err := os.WriteFile(target, []byte(opts.Content), 0o644); err != nil {
			return Result{}, fmt.Errorf("writing spec: %w", err)
		}
		return Result{Path: target, IsNew: true}, nil
	}
	if statErr != nil {
		return Result{}, fmt.Errorf("stat target: %w", statErr)
	}

	// File exists — check if git-tracked.
	absPath, err := filepath.Abs(target)
	if err != nil {
		return Result{}, fmt.Errorf("abs path: %w", err)
	}

	tracked := isGitTracked(absPath)

	if tracked {
		// Write new content first, then git diff shows old (index) vs new (working tree).
		if err := os.WriteFile(target, []byte(opts.Content), 0o644); err != nil {
			return Result{}, fmt.Errorf("writing spec: %w", err)
		}
		diff := gitDiff(absPath)
		return Result{Path: target, Diff: diff, IsNew: false}, nil
	}

	// Untracked: read old, write new, compute diff via system diff.
	oldContent, err := os.ReadFile(target)
	if err != nil {
		return Result{}, fmt.Errorf("reading existing spec: %w", err)
	}
	if err := os.WriteFile(target, []byte(opts.Content), 0o644); err != nil {
		return Result{}, fmt.Errorf("writing spec: %w", err)
	}

	diff, err := diffUnified(oldContent, absPath)
	if err != nil {
		return Result{}, fmt.Errorf("computing diff: %w", err)
	}

	return Result{Path: target, Diff: diff, IsNew: false}, nil
}

// missingH2Sections returns required section names absent from content.
// A section is present when a line starts with "## " and contains the section
// name as a case-insensitive substring.
func missingH2Sections(content string) []string {
	present := make(map[string]bool)
	for _, line := range strings.Split(content, "\n") {
		lower := strings.ToLower(line)
		if !strings.HasPrefix(lower, "## ") {
			continue
		}
		for _, req := range requiredSections {
			if strings.Contains(lower, req) {
				present[req] = true
			}
		}
	}
	var missing []string
	for _, req := range requiredSections {
		if !present[req] {
			missing = append(missing, req)
		}
	}
	return missing
}

// isGitTracked returns true if the file is tracked by git.
func isGitTracked(absPath string) bool {
	cmd := exec.Command("git", "-C", filepath.Dir(absPath), "ls-files", "--error-unmatch", "--", filepath.Base(absPath))
	return cmd.Run() == nil
}

// gitDiff returns the unified diff from git for a tracked file (working tree vs index).
func gitDiff(absPath string) string {
	cmd := exec.Command("git", "-C", filepath.Dir(absPath), "diff", "--", filepath.Base(absPath))
	out, _ := cmd.Output()
	return string(out)
}

// validSlug reports whether s is a safe kebab-case slug.
// Allows lowercase ASCII letters, digits, and interior hyphens.
// Rejects empty, leading/trailing hyphen, and any path-unsafe character.
var reValidSlug = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*[a-z0-9]$|^[a-z0-9]$`)

func validSlug(s string) bool {
	return reValidSlug.MatchString(s)
}

// diffUnified writes oldContent to a temp file and runs diff -u <tmp> <target>.
// Exit code 1 means files differ — that is not an error.
func diffUnified(oldContent []byte, targetAbs string) (string, error) {
	if _, err := exec.LookPath("diff"); err != nil {
		return "", fmt.Errorf("diff binary not found in PATH — cannot compute in-memory diff: %w", err)
	}
	tmp, err := os.CreateTemp("", "pakka-spec-old-*.md")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(oldContent); err != nil {
		tmp.Close()
		return "", err
	}
	tmp.Close()

	cmd := exec.Command("diff", "-u", "--", tmp.Name(), targetAbs)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			// exit 1 = files differ; not an error
			return string(out), nil
		}
		return "", err
	}
	return string(out), nil
}
