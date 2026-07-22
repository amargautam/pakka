package statusline

import (
	"testing"

	"github.com/amargautam/pakka/internal/benchratio"
	"github.com/amargautam/pakka/internal/hookevent"
)

// writeBenchRatio writes a single measured reduction entry to
// ~/.pakka/bench-ratios.json under home.
func writeBenchRatio(t *testing.T, home, repo, level string, ratio float64) {
	t.Helper()
	s := &benchratio.Store{}
	s.Record(repo, "claude-sonnet-4-6", level, ratio, "2026-07-22T00:00:00Z")
	if err := s.SaveTo(home); err != nil {
		t.Fatal(err)
	}
	// Rewrites within one process may reuse mtime+size; drop the render-path
	// memo so the next compute() observes the value just written.
	benchratio.ResetCache()
}

// TestMeasuredRatioChangesDollarFigure covers AC2: with identical telemetry, a
// measured ratio that differs from the calibrated constant produces a
// different $ figure and output %.
func TestMeasuredRatioChangesDollarFigure(t *testing.T) {
	home := t.TempDir()
	useFakeHome(t, home)
	useFakeRepoKey(t, map[string]string{"/repo/A": "/repo/A"})
	writeTranscriptDir(t, home, "-repo-A", "t1.jsonl", []map[string]int64{
		{"input_tokens": 0, "output_tokens": 100000},
	})
	event := &hookevent.Event{SessionID: "s1", CWD: "/repo/A"}

	// No bench-ratios.json -> calibrated constant (super-ultra: mult 1.94).
	base := compute(event, nil, "super-ultra", 0)

	// A measured ratio that is deliberately different from mult/(1+mult)=~0.66.
	writeBenchRatio(t, home, "/repo/A", "super-ultra", 0.30)
	measured := compute(event, nil, "super-ultra", 0)

	if base.savedUSD == measured.savedUSD {
		t.Errorf("presence of measured ratio must change $ figure: both %.6f", base.savedUSD)
	}
	if base.outPct == measured.outPct {
		t.Errorf("presence of measured ratio must change output %%: both %d", base.outPct)
	}
	// 0.30 reduction -> outPct 30.
	if measured.outPct != 30 {
		t.Errorf("measured outPct want 30, got %d", measured.outPct)
	}
	// Lower reduction (0.30 < ~0.66) -> lower output $ -> lower total.
	if measured.savedUSD >= base.savedUSD {
		t.Errorf("0.30 reduction should save less than ~0.66 constant: measured=%.6f base=%.6f",
			measured.savedUSD, base.savedUSD)
	}
}

// TestTwoRatiosTwoFigures covers AC4: two different stored ratios yield two
// different $ figures for the same telemetry.
func TestTwoRatiosTwoFigures(t *testing.T) {
	home := t.TempDir()
	useFakeHome(t, home)
	useFakeRepoKey(t, map[string]string{"/repo/A": "/repo/A"})
	writeTranscriptDir(t, home, "-repo-A", "t1.jsonl", []map[string]int64{
		{"input_tokens": 0, "output_tokens": 100000},
	})
	event := &hookevent.Event{SessionID: "s1", CWD: "/repo/A"}

	writeBenchRatio(t, home, "/repo/A", "super-ultra", 0.25)
	lo := compute(event, nil, "super-ultra", 0)

	// Overwrite with a different ratio.
	writeBenchRatio(t, home, "/repo/A", "super-ultra", 0.75)
	hi := compute(event, nil, "super-ultra", 0)

	if lo.savedUSD == hi.savedUSD {
		t.Errorf("distinct ratios must give distinct $: both %.6f", lo.savedUSD)
	}
	if hi.savedUSD <= lo.savedUSD {
		t.Errorf("higher reduction must save more: hi=%.6f lo=%.6f", hi.savedUSD, lo.savedUSD)
	}
	if lo.outPct != 25 || hi.outPct != 75 {
		t.Errorf("outPct should track ratio: lo=%d (want 25) hi=%d (want 75)", lo.outPct, hi.outPct)
	}
}
