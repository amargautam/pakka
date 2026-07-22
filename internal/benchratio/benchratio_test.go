package benchratio

import (
	"math"
	"testing"
)

const ts = "2026-07-22T00:00:00Z"

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// TestRecordPersistsAndReloads covers AC1: a run persists an entry; a reload
// sees it with samples=1.
func TestRecordPersistsAndReloads(t *testing.T) {
	home := t.TempDir()
	s := &Store{}
	s.Record("/repo/pakka", "claude-sonnet-4-6", "super-ultra", 0.5, ts)
	if err := s.SaveTo(home); err != nil {
		t.Fatal(err)
	}

	got, err := LoadFrom(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(got.Entries))
	}
	e := got.Entries[0]
	if e.Repo != "/repo/pakka" || e.Model != "claude-sonnet-4-6" || e.Level != "super-ultra" {
		t.Errorf("key mismatch: %+v", e)
	}
	if e.Samples != 1 || !approx(e.Ratio, 0.5) {
		t.Errorf("want samples=1 ratio=0.5, got samples=%d ratio=%v", e.Samples, e.Ratio)
	}
}

// TestSecondRecordIncrementsSamplesRunningMean covers AC1's second-run
// requirement and the running-mean merge rule.
func TestSecondRecordIncrementsSamplesRunningMean(t *testing.T) {
	s := &Store{}
	s.Record("r", "m", "super-ultra", 0.4, ts)
	s.Record("r", "m", "super-ultra", 0.6, ts)
	if len(s.Entries) != 1 {
		t.Fatalf("merge failed, want 1 entry, got %d", len(s.Entries))
	}
	e := s.Entries[0]
	if e.Samples != 2 {
		t.Errorf("want samples=2, got %d", e.Samples)
	}
	// running mean of 0.4 then 0.6 = 0.5
	if !approx(e.Ratio, 0.5) {
		t.Errorf("want running mean 0.5, got %v", e.Ratio)
	}

	// A third distinct value: mean(0.4,0.6,0.9) = (0.5*2 + 0.9)/3 = 0.6333...
	s.Record("r", "m", "super-ultra", 0.9, ts)
	if s.Entries[0].Samples != 3 || !approx(s.Entries[0].Ratio, (0.5*2+0.9)/3) {
		t.Errorf("third merge wrong: %+v", s.Entries[0])
	}
}

// TestDifferentKeysDoNotMerge asserts each repo+model+level is its own entry.
func TestDifferentKeysDoNotMerge(t *testing.T) {
	s := &Store{}
	s.Record("r1", "m", "super-ultra", 0.5, ts)
	s.Record("r2", "m", "super-ultra", 0.5, ts)
	s.Record("r1", "m", "ultra", 0.5, ts)
	if len(s.Entries) != 3 {
		t.Fatalf("want 3 distinct entries, got %d", len(s.Entries))
	}
}

// TestResolveTierPrecedence covers the resolution order: repo+model+level wins
// over model+level.
func TestResolveTierPrecedence(t *testing.T) {
	s := &Store{}
	s.Record("myrepo", "m", "super-ultra", 0.7, ts)    // repo-specific
	s.Record("otherrepo", "m", "super-ultra", 0.2, ts) // same model+level, other repo

	// Tier 1: repo-specific match.
	r, n, ok := s.Resolve("myrepo", "m", "super-ultra")
	if !ok || !approx(r, 0.7) || n != 1 {
		t.Errorf("tier1: want 0.7/1, got %v/%d ok=%v", r, n, ok)
	}

	// Tier 2: repo not present -> model+level aggregate across repos.
	r, n, ok = s.Resolve("absent", "m", "super-ultra")
	if !ok {
		t.Fatal("tier2: expected a match")
	}
	// sample-weighted mean of 0.7 and 0.2 = 0.45, total 2 samples.
	if !approx(r, 0.45) || n != 2 {
		t.Errorf("tier2: want 0.45/2, got %v/%d", r, n)
	}
}

// TestResolveModelWildcard asserts an empty model matches any recorded model
// (status line / RECEIPTS do not know the session model but still resolve).
func TestResolveModelWildcard(t *testing.T) {
	s := &Store{}
	s.Record("myrepo", "claude-sonnet-4-6", "super-ultra", 0.66, ts)
	r, n, ok := s.Resolve("myrepo", "", "super-ultra")
	if !ok || !approx(r, 0.66) || n != 1 {
		t.Errorf("wildcard: want 0.66/1, got %v/%d ok=%v", r, n, ok)
	}
}

// TestResolveMissingReturnsNotFound asserts absence signals the caller to use
// the calibrated constant.
func TestResolveMissingReturnsNotFound(t *testing.T) {
	s := &Store{}
	if _, _, ok := s.Resolve("r", "m", "super-ultra"); ok {
		t.Error("empty store must resolve not-found")
	}
	s.Record("r", "m", "ultra", 0.5, ts)
	if _, _, ok := s.Resolve("r", "m", "super-ultra"); ok {
		t.Error("level mismatch must resolve not-found")
	}
}

// TestClamp covers the [0,1) clamp applied to raw reduction fractions.
func TestClamp(t *testing.T) {
	if got := Clamp(-0.3); got != 0 {
		t.Errorf("negative clamp: want 0, got %v", got)
	}
	if got := Clamp(1.5); got >= 1 || got <= 0.99 {
		t.Errorf("over-1 clamp out of range: %v", got)
	}
	if got := Clamp(0.42); !approx(got, 0.42) {
		t.Errorf("in-range clamp altered value: %v", got)
	}
}

// TestLoadMissingFileIsEmpty asserts first-run (no file) is not an error.
func TestLoadMissingFileIsEmpty(t *testing.T) {
	got, err := LoadFrom(t.TempDir())
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if len(got.Entries) != 0 {
		t.Errorf("want empty store, got %d entries", len(got.Entries))
	}
}
