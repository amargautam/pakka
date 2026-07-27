package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/amargautam/pakka/internal/calibrate"
)

// CalibrateCmd implements the "calibrate" subcommand — the reviewer-gate
// calibration harness (spec: docs/specs/2026-07-27-reviewer-calibration.md).
//
// It lives in pakka-core ONLY (never pakka-hot): calibration is an explicit,
// off-hot-path measurement invoked via `make calibrate`. The claude CLI must be
// on PATH (OAuth session); absent → named skip, exit 0, nothing written. No API
// key is ever read.
type CalibrateCmd struct{}

func (c *CalibrateCmd) Name() string { return "calibrate" }

func (c *CalibrateCmd) Run(args []string) error {
	fs := flag.NewFlagSet("calibrate", flag.ContinueOnError)
	repoRoot := fs.String("repo-root", ".", "pakka repo root (resolves seeds/agents/results paths, stamps commit)")
	threshold := fs.Int("threshold", 80, "confidence floor for a finding to count (gate default 80)")
	timeout := fs.Int("seed-timeout", 300, "per-seed model timeout in seconds; a hung seed is marked timeout and the run continues")
	claudeBin := fs.String("claude-bin", "claude", "claude CLI binary")
	if err := fs.Parse(args); err != nil {
		return err
	}

	opts := calibrate.Options{
		RepoRoot:    *repoRoot,
		Threshold:   *threshold,
		SeedTimeout: time.Duration(*timeout) * time.Second,
		ClaudeBin:   *claudeBin,
	}

	err := calibrate.Run(opts)
	var skip *calibrate.SkipError
	if errors.As(err, &skip) {
		// Named skip: report on stderr, exit 0, write nothing.
		fmt.Fprintln(os.Stderr, skip.Reason)
		return nil
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "pakka: calibrate: %v\n", err)
		os.Exit(1)
	}
	return nil
}
