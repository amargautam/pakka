package report

import (
	"strings"
	"testing"

	"github.com/amargautam/pakka/internal/benchratio"
	"github.com/amargautam/pakka/internal/meter"
)

// TestReceiptsDefaultCalibrationDisclosure covers AC3: absent measured data,
// RECEIPTS discloses "default calibration".
func TestReceiptsDefaultCalibrationDisclosure(t *testing.T) {
	stats := &Stats{
		OutputTokensTotal: 1_000_000,
		ToolUseCounts:     map[string]int{},
	}
	md := FormatMarkdown(stats, "0.1.0-dev")
	if !strings.Contains(md, "default calibration") {
		t.Errorf("default path must disclose 'default calibration':\n%s", md)
	}
	if strings.Contains(md, "measured, n=") {
		t.Errorf("default path must not claim measured provenance:\n%s", md)
	}
}

// TestReceiptsMeasuredDisclosure covers AC3: with measured data the source
// string names the sample count.
func TestReceiptsMeasuredDisclosure(t *testing.T) {
	stats := &Stats{
		OutputTokensTotal:   1_000_000,
		ToolUseCounts:       map[string]int{},
		OutputRatioMeasured: true,
		OutputReduction:     0.42,
		OutputRatioSamples:  3,
	}
	md := FormatMarkdown(stats, "0.1.0-dev")
	if !strings.Contains(md, "measured, n=3") {
		t.Errorf("measured path must disclose 'measured, n=3':\n%s", md)
	}
}

// TestReceiptsMeasuredChangesDollars covers AC4 for RECEIPTS: two different
// measured reductions yield two different reported $ figures for identical
// telemetry, and both differ from the calibrated default.
func TestReceiptsMeasuredChangesDollars(t *testing.T) {
	base := &Stats{OutputTokensTotal: 1_000_000, ToolUseCounts: map[string]int{}}
	lo := &Stats{OutputTokensTotal: 1_000_000, ToolUseCounts: map[string]int{},
		OutputRatioMeasured: true, OutputReduction: 0.20, OutputRatioSamples: 1}
	hi := &Stats{OutputTokensTotal: 1_000_000, ToolUseCounts: map[string]int{},
		OutputRatioMeasured: true, OutputReduction: 0.80, OutputRatioSamples: 1}

	baseUSD := totalSavingsFromMarkdown(t, FormatMarkdown(base, "0.1.0-dev"))
	loUSD := totalSavingsFromMarkdown(t, FormatMarkdown(lo, "0.1.0-dev"))
	hiUSD := totalSavingsFromMarkdown(t, FormatMarkdown(hi, "0.1.0-dev"))

	if loUSD == hiUSD {
		t.Errorf("distinct measured reductions must give distinct $: both %.4f", loUSD)
	}
	if hiUSD <= loUSD {
		t.Errorf("higher reduction must save more: hi=%.4f lo=%.4f", hiUSD, loUSD)
	}
	if baseUSD == loUSD || baseUSD == hiUSD {
		t.Errorf("measured figures must differ from default calibration: base=%.4f lo=%.4f hi=%.4f",
			baseUSD, loUSD, hiUSD)
	}
}

// TestGatherResolvesMeasuredRatio covers the Gather wiring end-to-end: a
// bench-ratios.json entry keyed by the repo+level is picked up and surfaced in
// the generated RECEIPTS provenance string.
func TestGatherResolvesMeasuredRatio(t *testing.T) {
	home := t.TempDir()
	prev := benchratio.OverrideHome
	benchratio.OverrideHome = home
	t.Cleanup(func() { benchratio.OverrideHome = prev })

	repoRoot := t.TempDir() // not a git repo -> RepoKey canonicalizes to itself

	// Record a measured ratio under the canonical repo key + super-ultra.
	s := &benchratio.Store{}
	// meter.RepoKey canonicalizes; record under the resolved key by resolving
	// through the same path the report uses.
	s.Record(meter.RepoKey(repoRoot), "claude-sonnet-4-6", "super-ultra", 0.55, "2026-07-22T00:00:00Z")
	s.Record(meter.RepoKey(repoRoot), "claude-sonnet-4-6", "super-ultra", 0.55, "2026-07-22T00:00:00Z")
	if err := s.SaveTo(home); err != nil {
		t.Fatal(err)
	}

	meterDir := t.TempDir()
	auditDir := t.TempDir()
	stats, err := Gather(meterDir, auditDir, repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !stats.OutputRatioMeasured {
		t.Fatal("Gather did not pick up the measured ratio")
	}
	if stats.OutputRatioSamples != 2 {
		t.Errorf("want 2 samples, got %d", stats.OutputRatioSamples)
	}
	md := FormatMarkdown(stats, "0.1.0-dev")
	if !strings.Contains(md, "measured, n=2") {
		t.Errorf("RECEIPTS must disclose 'measured, n=2':\n%s", md)
	}
}
