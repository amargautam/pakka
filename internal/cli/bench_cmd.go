package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/amargautam/pakka/internal/bench"
)

// BenchCmd implements the "bench" subcommand (issue #13).
//
// Both arms run through Claude Code with the inherited OAuth session — no
// API key is used or required. The raw arm sets PAKKA_DISABLED=1 so every
// pakka hook exits immediately; the pakka arm runs with the plugin active.
type BenchCmd struct{}

func (c *BenchCmd) Name() string { return "bench" }

func (c *BenchCmd) Run(args []string) error {
	opts, err := parseBenchArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pakka: bench: %v\n", err)
		os.Exit(2)
	}
	if err := bench.Run(opts); err != nil {
		fmt.Fprintf(os.Stderr, "pakka: bench: %v\n", err)
		os.Exit(1)
	}
	return nil
}

// parseBenchArgs parses bench flags into bench.Options.
//
// Flags:
//
//	--corpus=PATH       corpus.json path (required)
//	--out=PATH          results JSON output path (required)
//	--limit=N           cap entries run
//	--mode=both|raw|pakka
//	--level=LEVEL       pin pakka-arm compression level (lite|strict|ultra|super-ultra)
//	--claude-bin=PATH   claude binary (default "claude")
//	--claude-arg=ARG    extra argv appended to every claude call (repeatable)
//	--timeout=SECONDS   per-call timeout (default 180)
//	--no-record         do not persist the measured ratio to bench-ratios.json
//	--verbose
func parseBenchArgs(args []string) (bench.Options, error) {
	opts := bench.Options{
		Mode:         "both",
		ClaudeBin:    "claude",
		Timeout:      180 * time.Second,
		RecordRatios: true,
	}
	for _, a := range args {
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
		case strings.HasPrefix(a, "--level="):
			opts.Level = strings.TrimPrefix(a, "--level=")
		case strings.HasPrefix(a, "--claude-bin="):
			opts.ClaudeBin = strings.TrimPrefix(a, "--claude-bin=")
		case strings.HasPrefix(a, "--claude-arg="):
			opts.ExtraArgs = append(opts.ExtraArgs, strings.TrimPrefix(a, "--claude-arg="))
		case strings.HasPrefix(a, "--timeout="):
			n, err := strconv.Atoi(strings.TrimPrefix(a, "--timeout="))
			if err == nil {
				opts.Timeout = time.Duration(n) * time.Second
			}
		case a == "--verbose":
			opts.Verbose = true
		case a == "--no-record":
			opts.RecordRatios = false
		}
	}

	if opts.CorpusPath == "" || opts.OutPath == "" {
		return opts, fmt.Errorf("--corpus and --out are required")
	}
	return opts, nil
}
