package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/amargautam/pakka/internal/specgenerate"
)

// SpecGenerateCmd implements the "spec-generate" subcommand.
type SpecGenerateCmd struct{}

func (c *SpecGenerateCmd) Name() string { return "spec-generate" }

func (c *SpecGenerateCmd) Run(args []string) error {
	fs := flag.NewFlagSet("spec-generate", flag.ContinueOnError)
	slug := fs.String("slug", "", "descriptive kebab name (required)")
	date := fs.String("date", "", "YYYY-MM-DD; default: today")
	specsDir := fs.String("specs-dir", "docs/specs/", "directory for spec files")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *slug == "" {
		fmt.Fprintln(os.Stderr, "pakka: spec-generate: --slug is required")
		os.Exit(1)
	}
	content, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("reading stdin: %w", err)
	}
	result, err := specgenerate.Generate(specgenerate.Options{
		Slug:     *slug,
		Date:     *date,
		SpecsDir: *specsDir,
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
