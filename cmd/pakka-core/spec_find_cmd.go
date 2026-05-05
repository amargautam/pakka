package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/amargautam/pakka/internal/specfind"
)

// SpecFindCmd implements the "spec-find" subcommand.
type SpecFindCmd struct{}

func (c *SpecFindCmd) Name() string { return "spec-find" }

// Run parses flags, detects branch/changed files when not provided, calls
// specfind.Find, and prints the matched spec path to stdout (empty line if
// no match). Always exits 0.
func (c *SpecFindCmd) Run(args []string) error {
	fs := flag.NewFlagSet("spec-find", flag.ContinueOnError)
	branch := fs.String("branch", "", "current git branch name")
	changed := fs.String("changed", "", "comma-separated list of changed files")
	spec := fs.String("spec", "", "override: use this spec file directly")
	specsDir := fs.String("specs-dir", "docs/specs/", "directory to scan for specs")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Auto-detect branch if not provided.
	br := *branch
	if br == "" {
		br = gitBranch()
	}

	// Auto-detect changed files if not provided.
	var changedFiles []string
	if *changed != "" {
		for _, f := range strings.Split(*changed, ",") {
			f = strings.TrimSpace(f)
			if f != "" {
				changedFiles = append(changedFiles, f)
			}
		}
	} else {
		changedFiles = gitCachedFiles()
	}

	result, err := specfind.Find(specfind.Options{
		SpecsDir: *specsDir,
		Branch:   br,
		Changed:  changedFiles,
		Override: *spec,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "pakka: spec-find: %v\n", err)
		return err
	}

	fmt.Println(result.Path)
	return nil
}

// gitBranch returns the current git branch name, or empty string on failure.
func gitBranch() string {
	out, err := exec.Command("git", "branch", "--show-current").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// gitCachedFiles returns the list of staged (cached) file paths, or nil on failure.
func gitCachedFiles() []string {
	out, err := exec.Command("git", "diff", "--cached", "--name-only").Output()
	if err != nil {
		return nil
	}
	var files []string
	for _, f := range strings.Split(string(out), "\n") {
		f = strings.TrimSpace(f)
		if f != "" {
			files = append(files, f)
		}
	}
	return files
}
