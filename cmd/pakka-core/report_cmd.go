package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/amargautam/pakka/internal/report"
)

// ReportCmd implements the "report" subcommand.
type ReportCmd struct{}

func (c *ReportCmd) Name() string { return "report" }
func (c *ReportCmd) Run(args []string) error {
	runReport()
	return nil
}

// --- report (Pass 5) ---

func runReport() {
	format := "md"
	repoRootFlag := ""
	for _, a := range os.Args[2:] {
		if strings.HasPrefix(a, "--format=") {
			format = strings.TrimPrefix(a, "--format=")
		}
		if strings.HasPrefix(a, "--repo-root=") {
			repoRootFlag = strings.TrimPrefix(a, "--repo-root=")
		}
	}
	_ = format // only md for now

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "pakka: report: %v\n", err)
		os.Exit(1)
	}
	meterDir := filepath.Join(home, ".pakka", "meter")
	auditDir := filepath.Join(home, ".pakka", "audit")

	var repoRoot string
	if repoRootFlag != "" {
		repoRoot, err = filepath.Abs(repoRootFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "pakka: report: --repo-root: %v\n", err)
			os.Exit(1)
		}
	} else {
		repoRoot, err = os.Getwd()
		if err != nil {
			repoRoot = "."
		}
	}

	stats, err := report.Gather(meterDir, auditDir, repoRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pakka: report: %v\n", err)
		os.Exit(1)
	}

	fmt.Print(report.FormatMarkdown(stats, version))
}
