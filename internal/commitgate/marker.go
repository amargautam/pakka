package commitgate

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// PassMarker is the JSON shape written to .pakka/reviews/last-pass-ts by
// `pakka-core review-pass`. It binds a review pass to the exact staged diff
// that was reviewed, so a fresh marker cannot authorize a different commit.
type PassMarker struct {
	TS         int64  `json:"ts"`         // unix epoch seconds when the pass was recorded
	DiffSHA256 string `json:"diffSHA256"` // sha256 hex of raw `git diff --cached` bytes
	Verdict    string `json:"verdict"`    // always "passed" for a marker that authorizes
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
