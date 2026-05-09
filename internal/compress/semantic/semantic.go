// Package semantic provides LLM-rewrite compression for prose-heavy text.
//
// Unlike the deterministic engine in the parent package, semantic mode calls
// out to a Rewriter (typically an Anthropic API client) to produce shorter
// prose, then runs a deterministic Validator to ensure no load-bearing region
// (code, URLs, paths, dates, version strings, env vars, critical markers) was
// modified. A cherry-pick retry asks the model to fix violations; if the
// validator still fails after retries, the original input is returned
// unchanged — never ship a partially corrupted file.
//
// Semantic mode never replaces deterministic mode; it is opt-in per call.
package semantic

import (
	"context"
	"errors"
)

// Level controls prompt template aggressiveness.
type Level string

const (
	LevelLite       Level = "lite"
	LevelStrict     Level = "strict"
	LevelUltra      Level = "ultra"
	LevelSuperUltra Level = "super-ultra"
)

// AllLevels lists every supported level in increasing aggressiveness.
//
// Purpose: Authoritative ordering for tests and CLI validation.
// Errors: None.
func AllLevels() []Level {
	return []Level{LevelLite, LevelStrict, LevelUltra, LevelSuperUltra}
}

// ParseLevel converts a string to a Level, defaulting to LevelUltra.
//
// LevelUltra is pakka's brand default — see memory/DECISIONS.md
// "Default output level: ultra (decided 2026-04-29)". Empty/unknown inputs
// fall back to ultra rather than strict so the CLI default stays aligned
// with loadOutputLevel() in cmd/pakka-core.
//
// Purpose: Safe level parsing for CLI flag values and skill arguments.
// Errors: Never errors; unknown strings map to LevelSuperUltra (intentional default).
func ParseLevel(s string) Level {
	switch Level(s) {
	case LevelLite, LevelStrict, LevelUltra, LevelSuperUltra:
		return Level(s)
	default:
		// super-ultra is the intentional default — see DECISIONS.md.
		return LevelSuperUltra
	}
}

// Rewriter is implemented by anything that can rewrite prose at a given Level.
//
// Production wires this to an Anthropic API client; tests inject a stub.
type Rewriter interface {
	Rewrite(ctx context.Context, input string, level Level) (string, error)
}

// FixRewriter is an optional extension. When supplied violations on a retry,
// implementers MUST regenerate text that preserves the listed regions
// verbatim. Rewriters that don't implement this fall back to plain Rewrite.
type FixRewriter interface {
	RewriteFix(ctx context.Context, input string, level Level, violations []Violation) (string, error)
}

// ErrValidatorFailed is wrapped by RunSemantic when retries exhaust and the
// rewritten output still fails validation. The unwrapped chain carries the
// remaining violations via Violations().
var ErrValidatorFailed = errors.New("semantic: validator failed after retries")

// FailedError is returned when retries exhaust. Callers receive the original
// input unchanged in the runner's first return value; this error reports
// which preservation regions were dropped so logging can be precise.
type FailedError struct {
	Remaining []Violation
}

// Error implements the error interface.
//
// Purpose: Compose a stable, low-cardinality message for logs.
// Errors: None.
func (e *FailedError) Error() string {
	return ErrValidatorFailed.Error()
}

// Unwrap exposes ErrValidatorFailed for errors.Is checks.
func (e *FailedError) Unwrap() error { return ErrValidatorFailed }

// Violations returns the validator findings that survived all retries.
//
// Purpose: Accessor used by callers (CLI debug log, tests).
// Errors: None.
func (e *FailedError) Violations() []Violation { return e.Remaining }
