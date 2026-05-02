package compress

import (
	"strings"
	"testing"
)

func TestLinguisticArticles(t *testing.T) {
	cases := []struct{ in, want string }{
		{"the file is large", "file is large"},
		{"open a file now", "open file now"},
		{"an error occurred", "error occurred"},
		{"The File Is Large", "File Is Large"},
	}
	for _, c := range cases {
		got := linguisticLine(c.in)
		if got != c.want {
			t.Errorf("linguisticLine(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestLinguisticFiller(t *testing.T) {
	cases := []struct{ in, want string }{
		{"I just need this", "I need this"},
		{"it really works well", "it works well"},
		{"simply run it", "run it"},
		{"very important", "important"},
		{"actually it works", "it works"},
		{"kind of slow", "slow"},
		{"sort of broken", "broken"},
		{"basically done", "done"},
	}
	for _, c := range cases {
		got := linguisticLine(c.in)
		if got != c.want {
			t.Errorf("linguisticLine(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestLinguisticHedging(t *testing.T) {
	cases := []struct{ in, want string }{
		{"I think it works", "it works"},
		{"I believe it works", "it works"},
		{"in my opinion, it works", "it works"},
		{"it seems broken", "broken"},
		{"maybe try again", "try again"},
		{"perhaps later", "later"},
	}
	for _, c := range cases {
		got := linguisticLine(c.in)
		if got != c.want {
			t.Errorf("linguisticLine(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestLinguisticPleasantries(t *testing.T) {
	cases := []struct{ in, want string }{
		{"please review this", "review this"},
		{"thanks.", ""},
		{"let me know.", ""},
		{"happy to help", "help"},
	}
	for _, c := range cases {
		got := linguisticLine(c.in)
		if got != c.want {
			t.Errorf("linguisticLine(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestLinguisticFragments(t *testing.T) {
	cases := []struct{ in, want string }{
		{"That is correct", "correct"},
		{"This is important", "important"},
		{"There is a bug", "bug"},
		{"There are issues", "issues"},
		{"It is broken", "broken"},
	}
	for _, c := range cases {
		got := linguisticLine(c.in)
		if got != c.want {
			t.Errorf("linguisticLine(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// --- Must-not-touch cases ---

func TestLinguisticPreservesInlineCode(t *testing.T) {
	got := linguisticLine("the `the_func` method is just great")
	if !strings.Contains(got, "`the_func`") {
		t.Errorf("should preserve inline code, got %q", got)
	}
	if strings.Contains(got, "the `") {
		t.Errorf("should drop article before code, got %q", got)
	}
}

func TestLinguisticPreservesURLs(t *testing.T) {
	input := "visit the https://example.com/the/path page"
	got := linguisticLine(input)
	if !strings.Contains(got, "https://example.com/the/path") {
		t.Errorf("should preserve URL, got %q", got)
	}
}

func TestLinguisticPreservesIdentifiers(t *testing.T) {
	input := "the auth_token and processData are important"
	got := linguisticLine(input)
	if !strings.Contains(got, "auth_token") {
		t.Errorf("should preserve underscore identifier, got %q", got)
	}
	if !strings.Contains(got, "processData") {
		t.Errorf("should preserve camelCase identifier, got %q", got)
	}
}

func TestLinguisticPreservesNumbers(t *testing.T) {
	input := "the 42 widgets and 3.14 value"
	got := linguisticLine(input)
	if !strings.Contains(got, "42") {
		t.Errorf("should preserve integer, got %q", got)
	}
	if !strings.Contains(got, "3.14") {
		t.Errorf("should preserve decimal, got %q", got)
	}
}

func TestLinguisticPreservesMarkers(t *testing.T) {
	input := "TODO fix the bug and FIXME later"
	got := linguisticLine(input)
	if !strings.Contains(got, "TODO") {
		t.Errorf("should preserve TODO, got %q", got)
	}
	if !strings.Contains(got, "FIXME") {
		t.Errorf("should preserve FIXME, got %q", got)
	}
}

func TestLinguisticCodeBlocks(t *testing.T) {
	input := "header\n```\nthe just really very a\n```\nfooter"
	got := applyLinguistic(input)
	if !strings.Contains(got, "the just really very a") {
		t.Errorf("code block content should be untouched, got %q", got)
	}
}

func TestLinguisticPreservesSPDX(t *testing.T) {
	input := "the Apache-2.0 license"
	got := linguisticLine(input)
	if !strings.Contains(got, "Apache-2.0") {
		t.Errorf("should preserve SPDX tag, got %q", got)
	}
}

// --- Integration: strict mode runs structural + linguistic ---

func TestStrictRunsLinguistic(t *testing.T) {
	input := "The file is just really important.\n\n\nI think it works.\n"
	r := Run(input, ModeStrict)

	if strings.Contains(r.Output, "The ") {
		t.Errorf("strict should drop articles, got %q", r.Output)
	}
	if strings.Contains(r.Output, "just") {
		t.Errorf("strict should drop filler, got %q", r.Output)
	}
	if strings.Contains(r.Output, "I think") {
		t.Errorf("strict should drop hedging, got %q", r.Output)
	}
	if r.Ratio <= 0 {
		t.Errorf("ratio should be positive, got %f", r.Ratio)
	}
}

// --- Benchmark samples ---

func TestBenchmarkSamples(t *testing.T) {
	samples := []struct {
		name string
		text string
	}{
		{"claude-md-prose", sampleClaudeMD},
		{"subagent-return", sampleSubagent},
	}

	for _, s := range samples {
		strict := Run(s.text, ModeStrict)

		t.Logf("%s: strict=%.1f%% (orig=%d strict=%d)",
			s.name, strict.Ratio,
			strict.OriginalSize, strict.CompressedSize)

		if strict.Ratio <= 0 {
			t.Errorf("%s: strict should produce positive savings, got %.1f%%",
				s.name, strict.Ratio)
		}
	}
}

const sampleClaudeMD = `# Project Guidelines

## Overview

This is a comprehensive guide for the development team. The project is basically
a web application that provides an API for managing user data. I think the
architecture is really well-designed, and it seems to be performing very well in
production.

## Code Standards

Please follow the established coding standards. The team should just use the
existing linters and formatters. It is really important that all changes go
through code review. Perhaps we should also consider adding more comprehensive
test coverage for the ` + "`validateInput`" + ` function.

## API Design

The API endpoints should be designed with simplicity in mind. I believe that the
REST conventions are the best approach for this project. There are several
important considerations:

1. The authentication system uses JWT tokens for session management.
2. An error response should always include a descriptive message.
3. The rate limiting is basically just a simple token bucket implementation.

## Deployment

This is the deployment guide for the application. The CI/CD pipeline is actually
sort of complex, but it simply works by running the test suite and then deploying
to the staging environment. Let me know if you have any questions about the
process. Thanks for reading this guide.

## TODO: Update the monitoring section
## FIXME: The database migration docs are outdated
`

const sampleSubagent = `I've completed the analysis of the proposed changes. I think the overall
approach is basically sound, but there are perhaps a few areas that could use
some improvement. Let me provide a detailed breakdown.

## Code Review Findings

I believe the changes to the authentication module are really well thought out.
The implementation is just a straightforward application of the OAuth 2.0
protocol. However, it seems that there are some edge cases that maybe haven't
been considered.

### Finding 1: Error Handling

The error handling in the ` + "`processRequest`" + ` function is actually kind of
incomplete. I think the function should really handle the case where the
` + "`auth_token`" + ` is expired. Currently, there is just a generic error message
returned. Perhaps a more specific error code would be helpful. The fix is simply
to add a check for token expiration before processing the request.

### Finding 2: Rate Limiting

There is a potential issue with the rate limiting implementation. The current
approach basically uses an in-memory counter, which is very problematic in a
distributed environment. I think the team should perhaps consider using Redis.
The ` + "`rateLimiter`" + ` function is actually sort of naive in its approach. It simply
checks if the counter exceeds the threshold, but it doesn't really account for
the sliding window. Maybe a more sophisticated approach would be better.

### Finding 3: Input Validation

This is an important security consideration. The input validation in the
` + "`/api/users`" + ` endpoint is basically non-existent. I believe the team should
really add proper validation. Please consider using a schema validation library.

The ` + "`validateInput`" + ` helper function should just check for:
1. The required fields are present and non-empty
2. The field types match the expected schema definition
3. An SQL injection attempt is detected and blocked

## Summary

I think the overall code quality is really good. The changes are basically ready
for merge, but perhaps the team should just address the findings above first.
Let me know if you need any clarification on these findings. Thanks for the
opportunity to review this code. Happy to help with any follow-up questions.
`
