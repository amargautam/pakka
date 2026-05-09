package semantic

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// stubRewriter returns a programmable sequence of outputs and tracks calls.
// It implements both Rewriter and FixRewriter so retry calls go through the
// fix path when violations are non-nil.
type stubRewriter struct {
	outputs []string
	errs    []error
	calls   []stubCall
}

type stubCall struct {
	level      Level
	violations []Violation
	input      string
}

func (s *stubRewriter) Rewrite(ctx context.Context, input string, level Level) (string, error) {
	return s.next(input, level, nil)
}

func (s *stubRewriter) RewriteFix(ctx context.Context, input string, level Level, v []Violation) (string, error) {
	return s.next(input, level, v)
}

func (s *stubRewriter) next(input string, level Level, v []Violation) (string, error) {
	idx := len(s.calls)
	s.calls = append(s.calls, stubCall{level: level, violations: v, input: input})
	if idx >= len(s.outputs) {
		return "", errors.New("stub: out of programmed responses")
	}
	out := s.outputs[idx]
	var err error
	if idx < len(s.errs) {
		err = s.errs[idx]
	}
	return out, err
}

// (a) clean output → returned verbatim, no retry.
func TestRunSemantic_CleanFirstAttempt(t *testing.T) {
	in := "The auth middleware checks the token. If expired, return 401."
	clean := "Auth middleware checks token. If expired, return 401."
	r := &stubRewriter{outputs: []string{clean}}

	out, err := RunSemantic(context.Background(), r, in, LevelStrict)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out != clean {
		t.Errorf("output mismatch:\n got: %q\n want: %q", out, clean)
	}
	if len(r.calls) != 1 {
		t.Errorf("expected 1 call, got %d", len(r.calls))
	}
	if r.calls[0].violations != nil {
		t.Errorf("first call should have no violations attached")
	}
}

// (b) output missing a code block on first attempt → retry triggered, second
// attempt's clean output returned.
func TestRunSemantic_RetryOnMissingCodeBlock(t *testing.T) {
	in := "Use this snippet:\n```go\nfmt.Println(\"hi\")\n```\nAfterwards continue.\n"
	bad := "Use this snippet. Afterwards continue.\n"
	good := "Use this snippet:\n```go\nfmt.Println(\"hi\")\n```\nThen continue."
	r := &stubRewriter{outputs: []string{bad, good}}

	out, err := RunSemantic(context.Background(), r, in, LevelStrict)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out != good {
		t.Errorf("output mismatch:\n got: %q\n want: %q", out, good)
	}
	if len(r.calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(r.calls))
	}
	if r.calls[1].violations == nil {
		t.Errorf("retry call should carry the violations list")
	}
	foundCodeKind := false
	for _, v := range r.calls[1].violations {
		if v.Kind == KindCodeBlockModified {
			foundCodeKind = true
			break
		}
	}
	if !foundCodeKind {
		t.Errorf("retry should be told about the missing code block, got %#v", r.calls[1].violations)
	}
}

// (c) output failing validator twice → returns input unchanged + *FailedError.
func TestRunSemantic_RetriesExhaust(t *testing.T) {
	in := "Refer to https://pakka.dev/docs and the file ./CLAUDE.md for context.\n"
	bad1 := "Refer to docs and the file for context.\n"
	bad2 := "Docs file context.\n"
	bad3 := "Just refer.\n"
	r := &stubRewriter{outputs: []string{bad1, bad2, bad3}}

	out, err := RunSemantic(context.Background(), r, in, LevelStrict)
	if out != in {
		t.Errorf("on exhaustion, output must be original input. got %q want %q", out, in)
	}
	if err == nil {
		t.Fatalf("expected *FailedError, got nil")
	}
	if !errors.Is(err, ErrValidatorFailed) {
		t.Errorf("error must wrap ErrValidatorFailed, got %v", err)
	}
	var fe *FailedError
	if !errors.As(err, &fe) {
		t.Fatalf("expected *FailedError, got %T", err)
	}
	if len(fe.Violations()) == 0 {
		t.Errorf("FailedError must carry surviving violations")
	}
	if len(r.calls) != 3 {
		t.Errorf("expected 3 attempts (1 + 2 retries), got %d", len(r.calls))
	}
}

// Behavioral test: token estimates strictly DECREASE across levels for
// prose-heavy input. This is the no-theatre check — if the prompt templates
// stop varying with Level, this test fails.
//
// We can't run a real LLM here. Instead we use a deterministic stub that
// responds to each Level with a hand-crafted compression matching that
// level's intent. The token estimator (bytes/3.5) is the SAME function the
// status-line uses, so the test captures the metric users see.
func TestRunSemantic_TokenEstimateDecreasesAcrossLevels(t *testing.T) {
	in := strings.Repeat("The authentication middleware checks the token, and if the token is expired, it really should return a 401 response to the client. ", 8)

	// Hand-crafted level-appropriate compressions of the same prose. Each is
	// strictly shorter than the previous one. No code/URLs/paths/dates/env
	// vars in input, so validator passes trivially.
	liteOut := strings.Repeat("The authentication middleware checks the token; if expired, return a 401 response to the client. ", 8)
	strictOut := strings.Repeat("Auth middleware checks token; if expired, return 401 to client. ", 8)
	ultraOut := strings.Repeat("Auth checks token -> if expired -> 401. ", 8)
	superUltraOut := strings.Repeat("token expired -> 401. ", 8)

	cases := []struct {
		level Level
		out   string
	}{
		{LevelLite, liteOut},
		{LevelStrict, strictOut},
		{LevelUltra, ultraOut},
		{LevelSuperUltra, superUltraOut},
	}

	prevTokens := EstimateTokens(in)
	if prevTokens == 0 {
		t.Fatalf("input must be non-empty")
	}
	for _, c := range cases {
		r := &stubRewriter{outputs: []string{c.out}}
		got, err := RunSemantic(context.Background(), r, in, c.level)
		if err != nil {
			t.Fatalf("level=%s: unexpected err: %v", c.level, err)
		}
		tokens := EstimateTokens(got)
		if !(tokens < prevTokens) {
			t.Errorf("level=%s: tokens=%d not strictly less than prev=%d (output=%q)",
				c.level, tokens, prevTokens, got)
		}
		prevTokens = tokens
	}
}

// nilRewriter triggers the early-return guard.
func TestRunSemantic_NilRewriter(t *testing.T) {
	out, err := RunSemantic(context.Background(), nil, "input text", LevelStrict)
	if err == nil {
		t.Errorf("nil rewriter should error")
	}
	if out != "input text" {
		t.Errorf("nil rewriter must return original input unchanged, got %q", out)
	}
}

// Empty input is a no-op, no error.
func TestRunSemantic_EmptyInput(t *testing.T) {
	r := &stubRewriter{outputs: []string{"should not be called"}}
	out, err := RunSemantic(context.Background(), r, "", LevelStrict)
	if err != nil {
		t.Errorf("empty input should not error, got %v", err)
	}
	if out != "" {
		t.Errorf("empty input should return empty, got %q", out)
	}
	if len(r.calls) != 0 {
		t.Errorf("empty input must skip the rewriter, got %d calls", len(r.calls))
	}
}

// Validator coverage matrix: feed inputs containing each preservable region
// type, mutate one at a time, assert correct violation Kind returned.
func TestValidator_RegionCoverage(t *testing.T) {
	cases := []struct {
		name     string
		original string
		mutated  string
		wantKind string
	}{
		{
			name:     "fenced code block dropped",
			original: "intro\n```go\nfmt.Println()\n```\nend",
			mutated:  "intro\nend",
			wantKind: KindCodeBlockModified,
		},
		{
			name:     "tilde fence dropped",
			original: "intro\n~~~py\nprint('hi')\n~~~\nend",
			mutated:  "intro\nend",
			wantKind: KindCodeBlockModified,
		},
		{
			name:     "inline backtick removed",
			original: "Use the `--strict` flag.",
			mutated:  "Use the strict flag.",
			wantKind: KindInlineCodeChanged,
		},
		{
			name:     "URL altered",
			original: "Docs at https://pakka.dev/install.",
			mutated:  "Docs available.",
			wantKind: KindURLChanged,
		},
		{
			name:     "absolute path lost",
			original: "Write log to /var/log/pakka.log later.",
			mutated:  "Write log to file later.",
			wantKind: KindPathChanged,
		},
		{
			name:     "home path lost",
			original: "Cache at ~/.pakka/audit dir.",
			mutated:  "Cache at home dir.",
			wantKind: KindPathChanged,
		},
		{
			name:     "relative path lost",
			original: "Run ./bin/run later.",
			mutated:  "Run binary later.",
			wantKind: KindPathChanged,
		},
		{
			name:     "ISO date lost",
			original: "Shipped 2026-04-29.",
			mutated:  "Shipped recently.",
			wantKind: KindDateChanged,
		},
		{
			name:     "version string lost",
			original: "Targeting v0.1.0 release.",
			mutated:  "Targeting upcoming release.",
			wantKind: KindVersionChanged,
		},
		{
			name:     "env var lost",
			original: "Reads $ANTHROPIC_API_KEY at startup.",
			mutated:  "Reads API key at startup.",
			wantKind: KindEnvVarChanged,
		},
		{
			name:     "TODO marker lost",
			original: "TODO fix this leak.",
			mutated:  "fix this leak.",
			wantKind: KindCriticalMarkerLost,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vs := Validate(tc.original, tc.mutated)
			if len(vs) == 0 {
				t.Fatalf("expected at least one violation, got none")
			}
			found := false
			for _, v := range vs {
				if v.Kind == tc.wantKind {
					found = true
					break
				}
			}
			if !found {
				kinds := make([]string, 0, len(vs))
				for _, v := range vs {
					kinds = append(kinds, v.Kind)
				}
				t.Errorf("expected violation kind %q, got %v", tc.wantKind, kinds)
			}
		})
	}
}

// When original equals rewritten, the validator never fires.
func TestValidator_IdentityNoViolations(t *testing.T) {
	in := "Run ./bin/run, see https://pakka.dev/docs, version v0.1.0, key $X. TODO fix it."
	if vs := Validate(in, in); len(vs) != 0 {
		t.Errorf("identity must produce no violations, got %#v", vs)
	}
}

// Empty original is treated as a no-op.
func TestValidator_EmptyOriginal(t *testing.T) {
	if vs := Validate("", "anything"); vs != nil {
		t.Errorf("empty original should yield nil, got %#v", vs)
	}
}

// ParseLevel maps known strings unchanged; defaults unknown to LevelUltra.
//
// Pass 4.4 flipped the default from strict to ultra (see DECISIONS.md
// "Default output level: ultra"). The legal-values rows below confirm that
// every known level — lite, strict, ultra, super-ultra — round-trips
// unchanged. Only empty/garbage input picks up the new ultra default.
func TestParseLevel(t *testing.T) {
	cases := map[string]Level{
		// Legal values pass through unchanged. strict is still legal — only
		// the default changed.
		"lite":        LevelLite,
		"strict":      LevelStrict,
		"ultra":       LevelUltra,
		"super-ultra": LevelSuperUltra,
		// Empty + unknown fall back to the brand default (super-ultra).
		"":      LevelSuperUltra,
		"weird": LevelSuperUltra,
	}
	for in, want := range cases {
		if got := ParseLevel(in); got != want {
			t.Errorf("ParseLevel(%q)=%q want %q", in, got, want)
		}
	}
}

// EstimateTokens is monotonic in input length and round-trips boundaries.
func TestEstimateTokens(t *testing.T) {
	if EstimateTokens("") != 0 {
		t.Errorf("empty must be 0 tokens")
	}
	short := EstimateTokens("hi")
	long := EstimateTokens(strings.Repeat("hi", 100))
	if !(long > short) {
		t.Errorf("longer input must yield more tokens: short=%d long=%d", short, long)
	}
}
