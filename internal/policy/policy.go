// Package policy loads and enforces a committed, team-standardized policy floor
// from <repo>/.pakka/policy.json.
//
// The policy file lets an engineering org set minimums the pakka binaries
// enforce directly (Go enforcement, not skill prompts) so that local
// settings.json / env opt-outs cannot weaken the gate the team standardized on.
// Enforcement is strict-direction only: policy can make the gate stricter, never
// looser.
//
// Design contract:
//   - Absent file → zero-value Policy ("no policy") + nil error. Callers behave
//     exactly as they did before policy existed (byte-identical).
//   - Malformed JSON, a newer schema version than supported, or an out-of-range
//     markerFreshnessSeconds → *PolicyError. Gate/guard callers fail CLOSED
//     (block) on a PolicyError; they fail OPEN (proceed) only on an absent file.
//   - Unknown keys → recorded in Warnings, load still succeeds (forward compat).
//
// stdlib-only by design: this package is linked into the lean pakka-hot binary
// via internal/commitgate and internal/guard, and must not drag in any of the
// forbidden heavy dependencies (see cmd/pakka-hot/nosqlite_test.go).
package policy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// CurrentVersion is the highest policy schema version this binary understands.
// A policy file declaring a higher version fails closed with an upgrade message.
const CurrentVersion = 1

// DefaultMarkerFreshnessSeconds is the marker-freshness window applied when no
// policy and no local setting narrow it. v0.19.0 raised this from the legacy
// flat 300s wall-clock: markers bind to the staged diff content (v0.15.0), so an
// unchanged diff should stay reviewed while the developer iterates, not expire on
// a five-minute timer.
const DefaultMarkerFreshnessSeconds = 1800

// MaxMarkerFreshnessSeconds is the hard ceiling on any freshness window. A policy
// value above it is rejected at parse (fail closed) so a policy file can never be
// used to widen the window past this bound.
const MaxMarkerFreshnessSeconds = 3600

// InputCompressLockedOff is the inputCompress value that forces SessionStart /
// orchestrator input-file compression off, ignoring any local opt-in.
const InputCompressLockedOff = "locked-off"

// fileName is the committed policy file's path relative to the repo root.
const relPath = ".pakka/policy.json"

// Policy is the typed, validated view of a repo's policy floor. The zero value is
// a valid "no policy" state (Present() == false): every enforcement helper is a
// no-op on it, which is what preserves pre-policy behavior when the file is
// absent.
type Policy struct {
	present bool

	// V is the declared schema version (0 when the file omitted "v").
	V int

	// ConfidenceThreshold, when > 0, caps the review gate's confidence
	// threshold DOWNWARD: local settings above it are clamped to it (policy
	// sets the maximum filtering, i.e. the minimum sensitivity the team accepts).
	ConfidenceThreshold int

	// MarkerFreshnessSeconds, when > 0, caps the marker-freshness window
	// DOWNWARD: a local window above it is clamped to it. Guaranteed ≤
	// MaxMarkerFreshnessSeconds by Load.
	MarkerFreshnessSeconds int

	// LockedCategories names guard heuristic categories (eval, traversal, …)
	// whose blocks can never be overridden by the learned per-repo allowlist.
	LockedCategories []string

	// InputCompress, when "locked-off", forces input-file compression off
	// regardless of env / settings opt-in.
	InputCompress string

	// Warnings holds non-fatal load diagnostics (e.g. unknown keys). Load still
	// succeeds; callers may surface these.
	Warnings []string
}

// PolicyError is a fatal policy-load failure. Gate and guard callers treat it as
// a hard block (fail closed): a policy the binary cannot safely interpret must
// stop commits, not be silently ignored.
type PolicyError struct {
	Path string
	Msg  string
}

func (e *PolicyError) Error() string {
	return fmt.Sprintf("pakka policy %s: %s", e.Path, e.Msg)
}

// knownKeys is the set of recognized top-level policy keys. Anything else is
// reported as a warning (forward compatibility) rather than rejected.
var knownKeys = map[string]bool{
	"v":                      true,
	"confidenceThreshold":    true,
	"markerFreshnessSeconds": true,
	"lockedCategories":       true,
	"inputCompress":          true,
}

// validCategories is the authoritative set of guard heuristic category names a
// policy may lock. It mirrors internal/guard's bashChecks pattern names plus the
// always-blocked "secrets" family. It lives here (not in guard) to avoid an
// import cycle (guard imports policy); a guard-side test asserts guard's pattern
// names stay a subset so the two never drift. An unknown lockedCategories entry
// is a typo that would silently lock nothing, so Load rejects it (fail closed) —
// an org authoring policy wants a loud error, not a warning.
var validCategories = map[string]bool{
	"eval":          true,
	"shell-c-eval":  true,
	"pipe-shell":    true,
	"download-exec": true,
	"traversal":     true,
	"system-path":   true,
	"secrets":       true,
}

// IsValidCategory reports whether name is a category a policy may lock. Exported
// so guard can assert its own pattern names are a subset (drift guard).
func IsValidCategory(name string) bool { return validCategories[name] }

// Load reads <repoRoot>/.pakka/policy.json and returns the validated Policy.
//
//   - repoRoot == "" or the file absent → zero Policy, nil error (no policy).
//   - unreadable existing file, malformed JSON, unknown/newer schema version, or
//     markerFreshnessSeconds out of range → *PolicyError (callers fail closed).
//   - unknown top-level keys → recorded in Policy.Warnings, load succeeds.
func Load(repoRoot string) (Policy, error) {
	if repoRoot == "" {
		return Policy{}, nil
	}
	path := filepath.Join(repoRoot, relPath)
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Policy{}, nil
	}
	if err != nil {
		// The file exists but cannot be read — fail closed rather than silently
		// treating a present policy as absent.
		return Policy{}, &PolicyError{Path: path, Msg: "cannot read policy file: " + err.Error()}
	}

	// First pass: detect unknown keys without failing on them.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return Policy{}, &PolicyError{Path: path, Msg: "malformed policy JSON: " + err.Error()}
	}
	var warnings []string
	for k := range raw {
		if !knownKeys[k] {
			warnings = append(warnings, fmt.Sprintf("unknown policy key %q ignored", k))
		}
	}

	// Second pass: typed decode. Pointers distinguish "absent" from "zero".
	var doc struct {
		V                      *int     `json:"v"`
		ConfidenceThreshold    *int     `json:"confidenceThreshold"`
		MarkerFreshnessSeconds *int     `json:"markerFreshnessSeconds"`
		LockedCategories       []string `json:"lockedCategories"`
		InputCompress          string   `json:"inputCompress"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return Policy{}, &PolicyError{Path: path, Msg: "malformed policy JSON: " + err.Error()}
	}

	// Validate lockedCategories against the known guard category set. A typo
	// would silently lock nothing, so fail closed and name the bad entry.
	for _, c := range doc.LockedCategories {
		if !validCategories[c] {
			return Policy{}, &PolicyError{Path: path, Msg: fmt.Sprintf(
				"unknown lockedCategories entry %q — valid categories: eval, shell-c-eval, pipe-shell, download-exec, traversal, system-path, secrets", c)}
		}
	}

	p := Policy{
		present:          true,
		LockedCategories: doc.LockedCategories,
		InputCompress:    doc.InputCompress,
		Warnings:         warnings,
	}
	if doc.V != nil {
		p.V = *doc.V
	}
	if p.V > CurrentVersion {
		return Policy{}, &PolicyError{Path: path, Msg: fmt.Sprintf(
			"schema version %d is newer than supported version %d — upgrade pakka to enforce this policy", p.V, CurrentVersion)}
	}
	if doc.ConfidenceThreshold != nil {
		p.ConfidenceThreshold = *doc.ConfidenceThreshold
	}
	if doc.MarkerFreshnessSeconds != nil {
		f := *doc.MarkerFreshnessSeconds
		if f < 0 || f > MaxMarkerFreshnessSeconds {
			return Policy{}, &PolicyError{Path: path, Msg: fmt.Sprintf(
				"markerFreshnessSeconds %d out of range [0, %d]", f, MaxMarkerFreshnessSeconds)}
		}
		p.MarkerFreshnessSeconds = f
	}
	return p, nil
}

// Present reports whether a policy file was loaded. The zero value returns false.
func (p Policy) Present() bool { return p.present }

// ClampConfidenceThreshold applies the policy floor to a local confidence
// threshold. Policy caps the value DOWNWARD (a local value above the policy is
// pulled down; a stricter/lower local value is left alone). Returns the effective
// value and whether a clamp occurred. No policy / unset → (local, false).
func (p Policy) ClampConfidenceThreshold(local int) (effective int, clamped bool) {
	if !p.present || p.ConfidenceThreshold == 0 {
		return local, false
	}
	if local > p.ConfidenceThreshold {
		return p.ConfidenceThreshold, true
	}
	return local, false
}

// ClampMarkerFreshness applies the policy floor to a local freshness window.
// Policy caps the window DOWNWARD (a local window above the policy is pulled
// down). Returns the effective seconds and whether a clamp occurred. No policy /
// unset → (local, false).
func (p Policy) ClampMarkerFreshness(local int) (effective int, clamped bool) {
	if !p.present || p.MarkerFreshnessSeconds == 0 {
		return local, false
	}
	if local > p.MarkerFreshnessSeconds {
		return p.MarkerFreshnessSeconds, true
	}
	return local, false
}

// IsCategoryLocked reports whether policy locks the named guard heuristic
// category, meaning its block can never be overridden by the learned allowlist.
func (p Policy) IsCategoryLocked(name string) bool {
	if !p.present {
		return false
	}
	for _, c := range p.LockedCategories {
		if c == name {
			return true
		}
	}
	return false
}

// InputCompressLockedOff reports whether policy forces input-file compression
// off, overriding any local opt-in.
func (p Policy) InputCompressLockedOff() bool {
	return p.present && p.InputCompress == InputCompressLockedOff
}
