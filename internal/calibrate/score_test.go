package calibrate

import (
	"math"
	"testing"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// AC2: expected-match by bug_class.
func TestScore_matchByBugClass(t *testing.T) {
	exp := Expected{Kind: "correctness", BugClass: "n-plus-1-query", File: "handlers/users.go", LineApprox: 18}
	// Finding on the WRONG line/file but the right bug_class still matches.
	findings := []Finding{
		{Kind: "correctness", File: "somewhere/else.go", Line: 999, BugClass: "n-plus-1-query", Confidence: 90},
	}
	s := Score(findings, exp, 80)
	if !s.Recalled || s.Matched != 1 {
		t.Fatalf("bug_class match: recalled=%v matched=%d, want recalled=true matched=1", s.Recalled, s.Matched)
	}
}

// AC2: match by file + line within ±5; beyond the window misses.
func TestScore_matchByFileLineWindow(t *testing.T) {
	exp := Expected{Kind: "correctness", BugClass: "off-by-one", File: "util/slice.go", LineApprox: 20}

	cases := []struct {
		name      string
		line      int
		file      string
		wantMatch bool
	}{
		{"exact", 20, "util/slice.go", true},
		{"plus5", 25, "util/slice.go", true},
		{"minus5", 15, "util/slice.go", true},
		{"plus6-miss", 26, "util/slice.go", false},
		{"basename-only", 22, "slice.go", true},    // path prefix forgiven
		{"wrong-file", 20, "util/other.go", false}, // different basename
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// bug_class blank on the finding so ONLY file+line can match.
			f := []Finding{{Kind: "correctness", File: c.file, Line: c.line, Confidence: 90}}
			s := Score(f, exp, 80)
			if s.Recalled != c.wantMatch {
				t.Fatalf("line=%d file=%s: recalled=%v, want %v", c.line, c.file, s.Recalled, c.wantMatch)
			}
		})
	}
}

// AC2: line_approx fallback when finding omits `line` but sets `line_approx`.
func TestScore_lineApproxFallback(t *testing.T) {
	exp := Expected{Kind: "security", File: "auth/login.py", LineApprox: 3}
	f := []Finding{{Kind: "security", File: "auth/login.py", LineApprox: 4, Confidence: 95}}
	if s := Score(f, exp, 80); !s.Recalled {
		t.Fatalf("line_approx fallback should match, got recalled=false")
	}
}

// AC2: clean-fixture findings count as false positives; a clean seed is never
// recalled.
func TestScore_cleanFixtureFP(t *testing.T) {
	clean := Expected{Kind: "none", ExpectedFindings: 0}

	// Two findings above threshold → false positive, kept=2.
	f := []Finding{
		{Kind: "correctness", File: "store/user.go", Line: 10, Confidence: 90},
		{Kind: "security", File: "store/user.go", Line: 12, Confidence: 85},
	}
	s := Score(f, clean, 80)
	if !s.FalsePositive || s.Kept != 2 || s.Recalled {
		t.Fatalf("clean FP: fp=%v kept=%d recalled=%v, want fp=true kept=2 recalled=false", s.FalsePositive, s.Kept, s.Recalled)
	}

	// No findings → not a false positive.
	if s := Score(nil, clean, 80); s.FalsePositive || s.Kept != 0 {
		t.Fatalf("clean no-finding: fp=%v kept=%d, want fp=false kept=0", s.FalsePositive, s.Kept)
	}
}

// AC2: confidence threshold filters findings before any counting.
func TestScore_thresholdFiltering(t *testing.T) {
	exp := Expected{Kind: "correctness", File: "db/user.go", LineApprox: 8}

	// Two findings match by location; one is below threshold and must be dropped.
	f := []Finding{
		{Kind: "correctness", File: "db/user.go", Line: 8, Confidence: 90}, // kept, matches
		{Kind: "correctness", File: "db/user.go", Line: 9, Confidence: 50}, // dropped
	}
	// At threshold 80: only the high-confidence finding survives.
	s := Score(f, exp, 80)
	if s.Kept != 1 || s.Matched != 1 || !s.Recalled {
		t.Fatalf("threshold 80: kept=%d matched=%d recalled=%v, want 1/1/true", s.Kept, s.Matched, s.Recalled)
	}

	// Lower the threshold to 40 → both survive, both match.
	s = Score(f, exp, 40)
	if s.Kept != 2 || s.Matched != 2 {
		t.Fatalf("threshold 40: kept=%d matched=%d, want 2/2", s.Kept, s.Matched)
	}

	// Raise the threshold above both → nothing kept, not recalled.
	s = Score(f, exp, 95)
	if s.Kept != 0 || s.Recalled {
		t.Fatalf("threshold 95: kept=%d recalled=%v, want 0/false", s.Kept, s.Recalled)
	}
}

// AC2: a finding with confidence 0 (field omitted) is below any positive
// threshold and never counts.
func TestScore_zeroConfidenceDropped(t *testing.T) {
	exp := Expected{Kind: "correctness", File: "db/user.go", LineApprox: 8}
	f := []Finding{{Kind: "correctness", File: "db/user.go", Line: 8}} // Confidence 0
	if s := Score(f, exp, 80); s.Kept != 0 || s.Recalled {
		t.Fatalf("zero-confidence: kept=%d recalled=%v, want 0/false", s.Kept, s.Recalled)
	}
}

// AC2: aggregation — different fixtures produce different rates.
func TestAggregated_variedRates(t *testing.T) {
	bug := func(matched, kept int) SeedScore {
		return SeedScore{IsClean: false, Kept: kept, Matched: matched, Recalled: matched > 0}
	}
	clean := func(kept int) SeedScore {
		return SeedScore{IsClean: true, Kept: kept, FalsePositive: kept > 0}
	}

	// 3 bug seeds: two recalled, one missed → recall 2/3.
	// Findings across bug seeds: matched 2+1=3, kept 2+3+1=6 → precision 3/6=0.5.
	// 2 clean seeds: 1 finding total → fpRate 1/2 = 0.5.
	scores := []SeedScore{
		bug(2, 3), // recalled, precision drag
		bug(1, 1), // recalled, clean precision
		bug(0, 2), // missed, adds to kept only
		clean(0),
		clean(1),
	}
	agg := Aggregated(scores)
	if agg.N != 3 || agg.NClean != 2 {
		t.Fatalf("counts: n=%d nClean=%d, want 3/2", agg.N, agg.NClean)
	}
	if !approx(agg.Recall, 2.0/3.0) {
		t.Fatalf("recall=%v, want 0.666...", agg.Recall)
	}
	if !approx(agg.Precision, 0.5) {
		t.Fatalf("precision=%v, want 0.5", agg.Precision)
	}
	if !approx(agg.FPRate, 0.5) {
		t.Fatalf("fpRate=%v, want 0.5", agg.FPRate)
	}

	// A DIFFERENT fixture set → different rates (variation requirement).
	scores2 := []SeedScore{
		bug(1, 1), // recalled, perfect precision
		bug(1, 1), // recalled
		clean(0),  // no FP
	}
	agg2 := Aggregated(scores2)
	if !approx(agg2.Recall, 1.0) || !approx(agg2.Precision, 1.0) || !approx(agg2.FPRate, 0.0) {
		t.Fatalf("agg2 = %+v, want recall=1 precision=1 fpRate=0", agg2)
	}
	if approx(agg.Recall, agg2.Recall) {
		t.Fatalf("rates did not vary across fixtures: %v == %v", agg.Recall, agg2.Recall)
	}
}

// AC2: zero-denominator rates report 0 with the count disclosing the reason.
func TestAggregated_zeroDenominators(t *testing.T) {
	// No findings emitted anywhere: recall 0, precision 0 (no kept), fpRate 0.
	scores := []SeedScore{
		{IsClean: false, Kept: 0},
		{IsClean: true, Kept: 0},
	}
	agg := Aggregated(scores)
	if agg.Precision != 0 || agg.FPRate != 0 || agg.Recall != 0 {
		t.Fatalf("zero denom: %+v, want all-zero rates", agg)
	}
	if agg.N != 1 || agg.NClean != 1 {
		t.Fatalf("counts n=%d nClean=%d, want 1/1", agg.N, agg.NClean)
	}
}
