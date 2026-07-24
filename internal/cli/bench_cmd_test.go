package cli

import (
	"testing"
	"time"
)

func TestBenchCmdName(t *testing.T) {
	cmd := &BenchCmd{}
	if cmd.Name() != "bench" {
		t.Errorf("Name() = %q; want %q", cmd.Name(), "bench")
	}
}

func TestBenchCmdImplementsCommand(t *testing.T) {
	var _ Command = &BenchCmd{}
}

// parseBenchArgs maps flags to options; values must vary with input.
func TestParseBenchArgs(t *testing.T) {
	opts, err := parseBenchArgs([]string{
		"--corpus=/c/corpus.json",
		"--out=/o/r.json",
		"--limit=3",
		"--mode=pakka",
		"--level=lite",
		"--claude-bin=/usr/local/bin/claude",
		"--claude-arg=--plugin-dir",
		"--claude-arg=/p/dir",
		"--timeout=42",
		"--verbose",
	})
	if err != nil {
		t.Fatalf("parseBenchArgs: %v", err)
	}
	if opts.CorpusPath != "/c/corpus.json" || opts.OutPath != "/o/r.json" {
		t.Errorf("paths: %+v", opts)
	}
	if opts.Limit != 3 || opts.Mode != "pakka" || opts.Level != "lite" || !opts.Verbose {
		t.Errorf("flags: %+v", opts)
	}
	if opts.ClaudeBin != "/usr/local/bin/claude" {
		t.Errorf("claude-bin: %q", opts.ClaudeBin)
	}
	if len(opts.ExtraArgs) != 2 || opts.ExtraArgs[0] != "--plugin-dir" || opts.ExtraArgs[1] != "/p/dir" {
		t.Errorf("claude-arg repeatable: %v", opts.ExtraArgs)
	}
	if opts.Timeout != 42*time.Second {
		t.Errorf("timeout: %v", opts.Timeout)
	}

	// Different input → different options.
	opts2, err := parseBenchArgs([]string{"--corpus=/x.json", "--out=/y.json"})
	if err != nil {
		t.Fatalf("parseBenchArgs minimal: %v", err)
	}
	if opts2.Level != "" || opts2.Mode != "both" || opts2.Timeout != 180*time.Second || len(opts2.ExtraArgs) != 0 {
		t.Errorf("defaults: %+v", opts2)
	}
}

func TestParseBenchArgs_requiredFlags(t *testing.T) {
	if _, err := parseBenchArgs([]string{"--out=/y.json"}); err == nil {
		t.Error("missing --corpus must fail")
	}
	if _, err := parseBenchArgs([]string{"--corpus=/x.json"}); err == nil {
		t.Error("missing --out must fail")
	}
}
