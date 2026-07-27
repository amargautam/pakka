// Package calibrate implements the reviewer-gate calibration harness
// (spec: docs/specs/2026-07-27-reviewer-calibration.md).
//
// It measures the four reviewer agents' precision and recall against the
// seeded-bug corpus (benchmarks/seeds/) so the gate's headline "catches bugs"
// claim becomes a rate, not a count. The scoring in this file is pure — it
// takes findings + ground truth + a confidence threshold and returns a verdict
// with NO model invocation — so AC2's unit tests run offline. The live runner
// (run.go) shells out to `claude -p`; scoring never does.
package calibrate

import (
	"path/filepath"
)

// Finding is one structured finding parsed from a reviewer agent's output.
//
// Only the subset used for scoring is modeled. The reviewer agents emit
// `line`; the seeded corpus records `line_approx`; both are accepted so a
// finding and an expected record can be compared regardless of which field
// carries the number. BugClass is scored when present but the live reviewer
// schema omits it — real recall is carried by the file+line window.
type Finding struct {
	Kind       string `json:"kind,omitempty"`
	Severity   string `json:"severity,omitempty"`
	File       string `json:"file,omitempty"`
	Line       int    `json:"line,omitempty"`
	LineApprox int    `json:"line_approx,omitempty"`
	BugClass   string `json:"bug_class,omitempty"`
	Confidence int    `json:"confidence,omitempty"`
	Rationale  string `json:"rationale,omitempty"`
}

// lineNum returns the finding's line, preferring `line` and falling back to
// `line_approx` when `line` is unset.
func (f Finding) lineNum() int {
	if f.Line != 0 {
		return f.Line
	}
	return f.LineApprox
}

// Expected mirrors a seed's expected.json ground truth. For bug seeds:
// BugClass + File + LineApprox describe the planted bug. For clean fixtures:
// Kind == "none" and ExpectedFindings == 0.
type Expected struct {
	Kind             string `json:"kind,omitempty"`
	Severity         string `json:"severity,omitempty"`
	File             string `json:"file,omitempty"`
	LineApprox       int    `json:"line_approx,omitempty"`
	BugClass         string `json:"bug_class,omitempty"`
	ExpectedFindings int    `json:"expected_findings,omitempty"`
	Description      string `json:"description,omitempty"`
}

// IsClean reports whether the expected record is a clean fixture (no bug
// planted). Clean fixtures set kind "none".
func (e Expected) IsClean() bool {
	return e.Kind == "none"
}

// SeedScore is the scored outcome for one seed after threshold filtering.
type SeedScore struct {
	IsClean bool `json:"-"`
	// Kept is the count of findings at or above the confidence threshold.
	Kept int `json:"kept"`
	// Matched is the count of kept findings that match the planted bug
	// (bug seeds only; always 0 for clean fixtures).
	Matched int `json:"matched"`
	// Recalled is true when a bug seed had at least one matching kept finding.
	// Always false for clean fixtures.
	Recalled bool `json:"recalled"`
	// FalsePositive is true when a clean fixture produced any kept finding.
	// Always false for bug seeds.
	FalsePositive bool `json:"falsePositive"`
}

// matchesExpected reports whether a finding matches the planted bug: by
// bug_class OR by (file basename AND line within ±5 of line_approx). Basename
// comparison forgives path-prefix differences between what the model emits and
// what the seed records.
func matchesExpected(f Finding, exp Expected) bool {
	if exp.BugClass != "" && f.BugClass != "" && f.BugClass == exp.BugClass {
		return true
	}
	if exp.File != "" && sameFile(f.File, exp.File) {
		if exp.LineApprox == 0 || absInt(f.lineNum()-exp.LineApprox) <= 5 {
			return true
		}
	}
	return false
}

// Score classifies one seed's findings against its ground truth at the given
// confidence threshold. Findings below the threshold are dropped before any
// counting — a low-confidence finding neither recalls a bug nor counts as a
// false positive. A finding with confidence 0 (agent omitted the field) is
// treated as below any positive threshold.
//
// Bug seed: Recalled = at least one kept finding matches; Matched = count of
// matching kept findings; FalsePositive = false.
// Clean fixture: FalsePositive = any kept finding; Recalled/Matched = 0/false.
func Score(findings []Finding, exp Expected, threshold int) SeedScore {
	s := SeedScore{IsClean: exp.IsClean()}
	for _, f := range findings {
		if f.Confidence < threshold {
			continue
		}
		s.Kept++
		if s.IsClean {
			continue
		}
		if matchesExpected(f, exp) {
			s.Matched++
		}
	}
	if s.IsClean {
		s.FalsePositive = s.Kept > 0
	} else {
		s.Recalled = s.Matched > 0
	}
	return s
}

// RunCounts discloses how many seeds actually backed the rates versus how many
// were dropped by infrastructure failure. A degraded run (many timeouts/errors)
// stays visible instead of silently deflating recall.
type RunCounts struct {
	// Scored is the number of seeds that produced a reviewer verdict and were
	// folded into the rates (excludes timeout/error seeds).
	Scored int `json:"scored"`
	// Timeout is the number of seeds excluded because they hit the per-seed
	// deadline.
	Timeout int `json:"timeout"`
	// Error is the number of seeds excluded because materialization or every
	// agent transport failed.
	Error int `json:"error"`
}

// Aggregate holds corpus-wide rates. Recall = recalled bug seeds / bug seeds.
// Precision = matched findings / all kept findings across bug seeds. FPRate =
// clean-fixture kept findings / clean runs (spec module decision — an average
// findings-per-clean-run, so it can exceed 1). N is the number of bug seeds
// backing Recall; NClean is the clean-run denominator. Only seeds that produced
// a reviewer verdict feed these rates — timeout/error seeds are excluded so an
// infrastructure failure never masquerades as a reviewer miss (Counts records
// how many were dropped).
type Aggregate struct {
	Recall    float64   `json:"recall"`
	Precision float64   `json:"precision"`
	FPRate    float64   `json:"fpRate"`
	N         int       `json:"n"`
	NClean    int       `json:"nClean"`
	Model     string    `json:"model,omitempty"`
	Counts    RunCounts `json:"counts"`
	// Degraded is true when a majority of scored seeds returned a non-empty
	// reviewer response but yielded zero parsed findings — the signature of a
	// systemic parse/format failure rather than genuine reviewer performance.
	// When set, the rates are not trustworthy as reviewer quality and the
	// report annotates them.
	Degraded bool `json:"degraded,omitempty"`
}

// DegradedByParseFailure reports whether a majority of scored seeds parsed
// nothing (non-empty response, zero findings) — the systemic-parse-failure
// signature. Requires at least one scored seed.
func DegradedByParseFailure(parsedNothing, scored int) bool {
	return scored > 0 && parsedNothing*2 > scored
}

// Aggregated folds per-seed scores into corpus rates. Rates whose denominator
// is zero are reported as 0 (e.g. precision with no findings emitted); the N /
// NClean counts disclose the denominators so a 0 is not mistaken for a
// measured floor.
func Aggregated(scores []SeedScore) Aggregate {
	var (
		bugSeeds, recalled       int
		keptOnBugs, matchedOnBug int
		cleanRuns, cleanFindings int
	)
	for _, s := range scores {
		if s.IsClean {
			cleanRuns++
			cleanFindings += s.Kept
			continue
		}
		bugSeeds++
		keptOnBugs += s.Kept
		matchedOnBug += s.Matched
		if s.Recalled {
			recalled++
		}
	}
	agg := Aggregate{N: bugSeeds, NClean: cleanRuns}
	if bugSeeds > 0 {
		agg.Recall = float64(recalled) / float64(bugSeeds)
	}
	if keptOnBugs > 0 {
		agg.Precision = float64(matchedOnBug) / float64(keptOnBugs)
	}
	if cleanRuns > 0 {
		agg.FPRate = float64(cleanFindings) / float64(cleanRuns)
	}
	return agg
}

// sameFile compares two paths by basename, case-sensitive. Models often emit a
// path missing a leading directory; basename comparison forgives that.
func sameFile(a, b string) bool {
	return filepath.Base(a) == filepath.Base(b)
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
