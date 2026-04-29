// Runner orchestrates a single semantic compress: rewrite, validate, retry.
//
// On validation failure the runner asks the model again (up to maxRetries
// times) with the violation list embedded so the model can put dropped
// regions back. If retries exhaust, the runner returns the ORIGINAL input
// unchanged together with a *FailedError carrying the surviving violations.
// This is the safety contract: we never ship a partially corrupted file.
package semantic

import (
	"context"
	"fmt"
)

// maxRetries is the cherry-pick retry budget. The first call is "attempt 0";
// retries 1 and 2 follow on validation failure. Total wall calls ≤ 3.
const maxRetries = 2

// RunSemantic runs the rewriter, validates, and (on failure) retries with a
// cherry-pick prompt. It returns the rewritten string on success, or the
// original input unchanged when retries exhaust.
//
// Purpose: Single entry point for semantic compression. Pure / synchronous;
// the Rewriter is the only side-effecting dependency.
// Errors: Returns a *FailedError when validation fails after all retries.
// The first return value carries the original input in that case (not the
// last attempt) — callers MUST NOT treat a non-nil error as "use the output
// anyway".
func RunSemantic(ctx context.Context, r Rewriter, input string, level Level) (string, error) {
	if r == nil {
		return input, fmt.Errorf("semantic: nil rewriter")
	}
	if input == "" {
		return input, nil
	}

	// Attempt 0: plain rewrite.
	output, err := r.Rewrite(ctx, input, level)
	if err != nil {
		return input, fmt.Errorf("semantic: rewrite: %w", err)
	}
	violations := Validate(input, output)
	if len(violations) == 0 {
		return output, nil
	}

	// Cherry-pick retries.
	for attempt := 1; attempt <= maxRetries; attempt++ {
		next, err := fixOnce(ctx, r, input, level, violations)
		if err != nil {
			return input, fmt.Errorf("semantic: retry %d: %w", attempt, err)
		}
		nextViolations := Validate(input, next)
		if len(nextViolations) == 0 {
			return next, nil
		}
		violations = nextViolations
	}

	return input, &FailedError{Remaining: violations}
}

// fixOnce sends a cherry-pick retry prompt. Rewriters that implement
// FixRewriter receive the structured violation list; others fall back to a
// plain Rewrite using the level's template (still imperfect, but better than
// nothing).
//
// Purpose: One retry attempt with violation context.
// Errors: Returns rewriter errors verbatim.
func fixOnce(ctx context.Context, r Rewriter, input string, level Level, violations []Violation) (string, error) {
	if fr, ok := r.(FixRewriter); ok {
		return fr.RewriteFix(ctx, input, level, violations)
	}
	return r.Rewrite(ctx, input, level)
}

// EstimateTokens returns a labeled token estimate using the documented
// pakka calibration: bytes / 3.5, rounded to the nearest int.
//
// Purpose: Common helper for tests and callers comparing levels.
// Errors: None.
func EstimateTokens(s string) int {
	if len(s) == 0 {
		return 0
	}
	// Round to nearest: (bytes*2 + 7) / 7 == round(bytes/3.5).
	return (len(s)*2 + 7) / 7
}
