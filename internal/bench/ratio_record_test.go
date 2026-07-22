package bench

import (
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/amargautam/pakka/internal/benchratio"
	"github.com/amargautam/pakka/internal/meter"
)

// TestRunPersistsMeasuredRatio covers AC1: a "both"-arm run with RecordRatios
// persists a ratio entry to bench-ratios.json, and a second run increments the
// sample count for the same key.
func TestRunPersistsMeasuredRatio(t *testing.T) {
	home := t.TempDir()
	prev := benchratio.OverrideHome
	benchratio.OverrideHome = home
	t.Cleanup(func() { benchratio.OverrideHome = prev })

	dir := t.TempDir()
	corpusPath := writeFixtureCorpus(t, dir, 1)
	repo := meter.RepoKey(dir)

	newRunner := func() *fakeRunner {
		return &fakeRunner{
			responses: map[string]string{"raw": "", "pakka": ""},
			// raw out=100, pakka out=40 -> pakka/raw = 0.4 -> reduction 0.6.
			usages: map[string]Usage{
				"raw":   {InputTokens: 60, OutputTokens: 100, Model: "claude-sonnet-4-6"},
				"pakka": {InputTokens: 60, OutputTokens: 40, Model: "claude-sonnet-4-6"},
			},
		}
	}

	opts := Options{
		CorpusPath:   corpusPath,
		OutPath:      filepath.Join(dir, "out1.json"),
		Mode:         "both",
		Timeout:      time.Second,
		RecordRatios: true,
	}
	if err := run(opts, newRunner()); err != nil {
		t.Fatalf("run 1: %v", err)
	}

	store, err := benchratio.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(store.Entries) != 1 {
		t.Fatalf("want 1 persisted entry, got %d", len(store.Entries))
	}
	e := store.Entries[0]
	if e.Repo != repo || e.Model != "claude-sonnet-4-6" || e.Level != "super-ultra" {
		t.Errorf("key mismatch: %+v (want repo=%s)", e, repo)
	}
	if e.Samples != 1 {
		t.Errorf("first run: want samples=1, got %d", e.Samples)
	}
	if math.Abs(e.Ratio-0.6) > 1e-9 {
		t.Errorf("first run: want reduction 0.6, got %v", e.Ratio)
	}

	// Second run: same key -> samples increments to 2.
	opts.OutPath = filepath.Join(dir, "out2.json")
	if err := run(opts, newRunner()); err != nil {
		t.Fatalf("run 2: %v", err)
	}
	store2, err := benchratio.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(store2.Entries) != 1 {
		t.Fatalf("second run must merge, not append: got %d entries", len(store2.Entries))
	}
	if store2.Entries[0].Samples != 2 {
		t.Errorf("second run: want samples=2, got %d", store2.Entries[0].Samples)
	}
	if math.Abs(store2.Entries[0].Ratio-0.6) > 1e-9 {
		t.Errorf("second run: running mean should stay 0.6, got %v", store2.Entries[0].Ratio)
	}
}

// TestRunNoRecordByDefault asserts a run with RecordRatios unset never writes
// the ratios file (protects the user's home in unit tests).
func TestRunNoRecordByDefault(t *testing.T) {
	home := t.TempDir()
	prev := benchratio.OverrideHome
	benchratio.OverrideHome = home
	t.Cleanup(func() { benchratio.OverrideHome = prev })

	dir := t.TempDir()
	corpusPath := writeFixtureCorpus(t, dir, 1)

	fr := &fakeRunner{
		responses: map[string]string{"raw": "", "pakka": ""},
		usages: map[string]Usage{
			"raw":   {OutputTokens: 100},
			"pakka": {OutputTokens: 40},
		},
	}
	opts := Options{CorpusPath: corpusPath, OutPath: filepath.Join(dir, "out.json"), Mode: "both", Timeout: time.Second}
	if err := run(opts, fr); err != nil {
		t.Fatalf("run: %v", err)
	}
	store, _ := benchratio.Load()
	if len(store.Entries) != 0 {
		t.Errorf("RecordRatios unset must not persist: got %d entries", len(store.Entries))
	}
}
