package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/amargautam/pakka/internal/bench"
)

// BenchCmd implements the "bench" subcommand.
type BenchCmd struct{}

func (c *BenchCmd) Name() string { return "bench" }
func (c *BenchCmd) Run(args []string) error {
	runBench()
	return nil
}

// --- bench (Pass 5b) ---

func runBench() {
	opts := bench.Options{
		Mode:      "both",
		ClaudeBin: "claude",
		Timeout:   180 * time.Second,
	}
	for _, a := range os.Args[2:] {
		switch {
		case strings.HasPrefix(a, "--corpus="):
			opts.CorpusPath = strings.TrimPrefix(a, "--corpus=")
		case strings.HasPrefix(a, "--out="):
			opts.OutPath = strings.TrimPrefix(a, "--out=")
		case strings.HasPrefix(a, "--limit="):
			n, err := strconv.Atoi(strings.TrimPrefix(a, "--limit="))
			if err == nil {
				opts.Limit = n
			}
		case strings.HasPrefix(a, "--mode="):
			opts.Mode = strings.TrimPrefix(a, "--mode=")
		case strings.HasPrefix(a, "--claude-bin="):
			opts.ClaudeBin = strings.TrimPrefix(a, "--claude-bin=")
		case strings.HasPrefix(a, "--timeout="):
			n, err := strconv.Atoi(strings.TrimPrefix(a, "--timeout="))
			if err == nil {
				opts.Timeout = time.Duration(n) * time.Second
			}
		case a == "--verbose":
			opts.Verbose = true
		}
	}

	if opts.CorpusPath == "" || opts.OutPath == "" {
		fmt.Fprintf(os.Stderr, "pakka: bench: --corpus and --out are required\n")
		os.Exit(2)
	}

	if err := bench.Run(opts); err != nil {
		fmt.Fprintf(os.Stderr, "pakka: bench: %v\n", err)
		os.Exit(1)
	}
}
