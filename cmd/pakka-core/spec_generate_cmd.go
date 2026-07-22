package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/amargautam/pakka/internal/specgenerate"
)

// SpecGenerateCmd implements the "spec-generate" subcommand.
type SpecGenerateCmd struct{}

func (c *SpecGenerateCmd) Name() string { return "spec-generate" }

func (c *SpecGenerateCmd) Run(args []string) error {
	fs := flag.NewFlagSet("spec-generate", flag.ContinueOnError)
	slug := fs.String("slug", "", "descriptive kebab name (required)")
	date := fs.String("date", "", "YYYY-MM-DD; default: today")
	specsDir := fs.String("specs-dir", "docs/specs/", "spec directory, relative to the repo root unless absolute")
	repoRootFlag := fs.String("repo-root", "", "target repo root (default: git root of CWD)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *slug == "" {
		fmt.Fprintln(os.Stderr, "pakka: spec-generate: --slug is required")
		os.Exit(1)
	}

	// Anchor the spec to a git repo toplevel before reading stdin, so an
	// ambiguous location fails fast without consuming input.
	dir, err := resolveSpecsDir(*repoRootFlag, *specsDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pakka: spec-generate: %v\n", err)
		os.Exit(1)
	}

	content, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("reading stdin: %w", err)
	}
	result, err := specgenerate.Generate(specgenerate.Options{
		Slug:     *slug,
		Date:     *date,
		SpecsDir: dir,
		Content:  string(content),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "pakka: spec-generate: %v\n", err)
		os.Exit(1)
	}
	if result.Diff != "" {
		fmt.Println(result.Diff)
	}
	if result.IsNew {
		fmt.Printf("Spec written to %s\n", result.Path)
	} else {
		fmt.Printf("Spec updated at %s\n", result.Path)
	}
	return nil
}

// resolveSpecsDir resolves the directory spec files are written to, refusing
// ambiguous locations. It anchors the spec to a git repo toplevel so a drifted
// shell CWD cannot silently write the spec into the wrong sibling repo (the
// incident this guard prevents).
//
//   - repoRootFlag == "": resolve the git toplevel of the CWD; no repo -> error.
//   - repoRootFlag != "": the path must be an existing directory inside a git
//     repository; that path's git toplevel is used. A nonexistent or non-repo
//     path -> error.
//
// A relative specsDir (the default "docs/specs/") is joined to the resolved
// toplevel, so invoking from a subdirectory still writes to
// <toplevel>/docs/specs. An absolute specsDir is honored verbatim.
func resolveSpecsDir(repoRootFlag, specsDir string) (string, error) {
	if specsDir == "" {
		specsDir = "docs/specs/"
	}

	var top string
	if repoRootFlag != "" {
		info, statErr := os.Stat(repoRootFlag)
		if statErr != nil || !info.IsDir() {
			return "", fmt.Errorf("--repo-root %q is not an existing directory", repoRootFlag)
		}
		top = repoRootAt(repoRootFlag)
		if top == "" {
			return "", fmt.Errorf("--repo-root %q is not inside a git repository", repoRootFlag)
		}
	} else {
		top = repoRoot()
		if top == "" {
			return "", fmt.Errorf("not inside a git repository — cd into the target repo or pass --repo-root")
		}
	}

	if filepath.IsAbs(specsDir) {
		return specsDir, nil
	}
	return filepath.Join(top, specsDir), nil
}
