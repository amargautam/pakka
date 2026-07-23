package commitgate

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// FindingsCounts tallies the review findings by severity. total counts every
// parsed finding row (including rows with an unknown/absent severity); the
// per-severity fields count only their exact match.
type FindingsCounts struct {
	Error   int `json:"error"`
	Warning int `json:"warning"`
	Info    int `json:"info"`
	Total   int `json:"total"`
}

// PassMarker is the JSON shape written to .pakka/reviews/last-pass-ts by
// `pakka-core review-pass`. It binds a review pass to the exact staged diff
// that was reviewed, so a fresh marker cannot authorize a different commit.
//
// The findings* fields (v0.16) are populated only when review-pass is invoked
// with --findings; they bind the review EVIDENCE (the findings JSONL) to the
// marker. All three carry omitempty so a marker recorded without --findings is
// byte-for-byte identical to the v0.15 shape {ts, diffSHA256, verdict}.
type PassMarker struct {
	TS         int64  `json:"ts"`         // unix epoch seconds when the pass was recorded
	DiffSHA256 string `json:"diffSHA256"` // sha256 hex of raw `git diff --cached` bytes
	Verdict    string `json:"verdict"`    // always "passed" for a marker that authorizes

	// FindingsSHA256 is the sha256 hex of the findings file's raw bytes. When
	// non-empty the gate re-hashes the findings file at commit time and blocks
	// if it changed or vanished (evidence cannot be swapped post-approval).
	FindingsSHA256 string `json:"findingsSHA256,omitempty"`
	// FindingsPath is the findings file location relative to the git toplevel,
	// so the gate can resolve it from the repo root at commit time.
	FindingsPath string `json:"findingsPath,omitempty"`
	// FindingsCounts is the severity tally parsed from the findings file.
	FindingsCounts *FindingsCounts `json:"findingsCounts,omitempty"`
}

// ParseMarker unmarshals a last-pass-ts marker's content. ok is false when the
// content is not a JSON marker (empty, legacy bare-epoch, or garbage).
func ParseMarker(content string) (PassMarker, bool) {
	var m PassMarker
	if json.Unmarshal([]byte(strings.TrimSpace(content)), &m) != nil {
		return PassMarker{}, false
	}
	if m.DiffSHA256 == "" {
		return PassMarker{}, false
	}
	return m, true
}

// ReviewVerdict is the payload written to the session audit log (kind
// "review-verdict") when a gate pass is authorized by a findings-bound marker.
// Recall's Index picks it up with zero schema change; the concatenated
// Rationales make the findings' prose searchable via FTS5.
type ReviewVerdict struct {
	DiffSHA256     string         `json:"diffSHA256"`
	FindingsSHA256 string         `json:"findingsSHA256"`
	Counts         FindingsCounts `json:"counts"`
	Rationales     string         `json:"rationales"`
}

// MarkerClass classifies a last-pass-ts marker against the current staged diff.
type MarkerClass int

const (
	// MarkerNone: no marker, empty, or unparseable content.
	MarkerNone MarkerClass = iota
	// MarkerPass: fresh JSON marker whose diffSHA256 matches the current
	// staged diff — the only class that authorizes a commit.
	MarkerPass
	// MarkerStale: JSON marker older than the freshness window. Treated as
	// no pass (generic gate message).
	MarkerStale
	// MarkerMismatch: fresh JSON marker whose diffSHA256 does NOT match the
	// current staged diff — the review covered different changes.
	MarkerMismatch
	// MarkerLegacy: pre-JSON bare-epoch (or RFC3339) marker. Rejected with an
	// upgrade message; these markers are not diff-bound.
	MarkerLegacy
)

// ClassifyMarker inspects a last-pass-ts marker's content and returns how it
// bears on the gate decision. Pure — the caller supplies the file content, the
// sha256 hex of the current staged diff, the current unix time, and the
// freshness window in seconds.
//
// Rules:
//   - JSON marker with a non-empty diffSHA256 and verdict "passed":
//     stale (age > maxAgeSeconds) → MarkerStale; diff hash matches → MarkerPass;
//     otherwise → MarkerMismatch.
//   - bare integer epoch or RFC3339 timestamp → MarkerLegacy.
//   - empty / anything else → MarkerNone.
func ClassifyMarker(content, currentDiffSHA256 string, nowUnix, maxAgeSeconds int64) MarkerClass {
	raw := strings.TrimSpace(content)
	if raw == "" {
		return MarkerNone
	}

	// JSON marker (new, diff-bound format).
	var m PassMarker
	if json.Unmarshal([]byte(raw), &m) == nil && m.DiffSHA256 != "" {
		if m.Verdict != "passed" {
			return MarkerNone
		}
		if nowUnix-m.TS > maxAgeSeconds {
			return MarkerStale
		}
		if m.DiffSHA256 == currentDiffSHA256 {
			return MarkerPass
		}
		return MarkerMismatch
	}

	// Legacy bare-epoch marker (pre-diff-binding).
	if _, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return MarkerLegacy
	}
	// Legacy RFC3339 marker (also pre-diff-binding).
	if _, err := time.Parse(time.RFC3339, raw); err == nil {
		return MarkerLegacy
	}

	return MarkerNone
}
