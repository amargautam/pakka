package commitgate

import (
	"os/exec"
	"strings"
	"testing"
)

// TestEvaluate_AST pins acceptance criteria 1–8 from
// docs/specs/2026-06-02-v090-commit-gate-ast-receipts-drift.md.
//
// Each row is an independent input shape; assertions run per-row so a
// regression in any single shape surfaces directly, not collapsed into a
// single boolean. Per memory: feedback_measurement_first.md.
//
// Allow=true rows assert:
//   - Decision.Allow == true
//   - Decision.Command is non-empty and contains the trailer flag
//   - Decision.Command preserves the original chain/redirect/subshell
//     construct verbatim (so the user's intent isn't silently mutated)
//   - The rewritten command parses under `bash -n` (free syntax check)
//
// Allow=false rows assert:
//   - Decision.Allow == false
//   - Decision.Stderr names the rejection cause (eval / dynamic command name)
func TestEvaluate_AST(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CoAuthor = false // simplify trailer counting per node
	state := &State{HasRecentPass: true}

	const trailerFlag = "--trailer "
	const trailerKey = "Reviewed-by-pakka:"

	tests := []struct {
		name string
		cmd  string
		// allow=true cases:
		wantAllow      bool
		wantPreserved  []string // substrings that must survive the rewrite
		wantTrailers   int      // number of --trailer flags expected (1 default; 2 for chained-dup)
		wantBashSyntax bool     // run bash -n on the rewrite
		// allow=false cases:
		wantCauseSubstr string // substring of Stderr that names the cause
	}{
		{
			name:           "criterion 1: and-chain commit+push",
			cmd:            `git add -A && git commit -m "msg"`,
			wantAllow:      true,
			wantPreserved:  []string{"git add -A", "&&"},
			wantTrailers:   1,
			wantBashSyntax: true,
		},
		{
			name:           "criterion 2: surrounded by make-test and push",
			cmd:            `make test && git commit -m "msg" && git push`,
			wantAllow:      true,
			wantPreserved:  []string{"make test", "git push", "&&"},
			wantTrailers:   1,
			wantBashSyntax: true,
		},
		{
			name:           "criterion 3: env-prefixed commit",
			cmd:            `GIT_AUTHOR_DATE=2026-01-01 git commit -m "msg"`,
			wantAllow:      true,
			wantPreserved:  []string{"GIT_AUTHOR_DATE=2026-01-01"},
			wantTrailers:   1,
			wantBashSyntax: true,
		},
		{
			name:           "criterion 4: subshell with cd",
			cmd:            `(cd /tmp/repo && git commit -m "msg")`,
			wantAllow:      true,
			wantPreserved:  []string{"(", ")", "cd /tmp/repo"},
			wantTrailers:   1,
			wantBashSyntax: true,
		},
		{
			name:           "criterion 5: redirected commit",
			cmd:            `git commit -m "x" > /dev/null 2>&1`,
			wantAllow:      true,
			wantPreserved:  []string{">", "/dev/null", "2>&1"},
			wantTrailers:   1,
			wantBashSyntax: true,
		},
		{
			name:           "criterion 6: chained dup",
			cmd:            `git commit -m "a" && git commit -m "b"`,
			wantAllow:      true,
			wantPreserved:  []string{"&&"},
			wantTrailers:   2,
			wantBashSyntax: true,
		},
		{
			name:            "criterion 7: eval wrapper blocked, stderr names eval",
			cmd:             `eval "git commit -m msg"`,
			wantAllow:       false,
			wantCauseSubstr: "eval",
		},
		{
			name:            "criterion 8: dynamic command name blocked",
			cmd:             `$(echo git commit) -m msg`,
			wantAllow:       false,
			wantCauseSubstr: "dynamic command name",
		},
		{
			name:            "criterion 9: xargs git commit blocked (gate bypass)",
			cmd:             `echo x | xargs git commit -m msg`,
			wantAllow:       false,
			wantCauseSubstr: "git commit via xargs",
		},
		{
			name:            "criterion 10: env git commit blocked (gate bypass)",
			cmd:             `env git commit -m msg`,
			wantAllow:       false,
			wantCauseSubstr: "git commit via env",
		},
		{
			name:            "criterion 11: sudo git commit blocked (gate bypass)",
			cmd:             `sudo git commit -m msg`,
			wantAllow:       false,
			wantCauseSubstr: "git commit via sudo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := Evaluate(tt.cmd, cfg, state)

			if d.Allow != tt.wantAllow {
				t.Fatalf("Allow = %v, want %v\n  cmd=%q\n  stderr=%q\n  cmdOut=%q",
					d.Allow, tt.wantAllow, tt.cmd, d.Stderr, d.Command)
			}

			if !tt.wantAllow {
				if d.Stderr == "" {
					t.Fatalf("blocked but Stderr is empty; expected mention of %q", tt.wantCauseSubstr)
				}
				if !strings.Contains(d.Stderr, tt.wantCauseSubstr) {
					t.Fatalf("Stderr = %q, missing cause substring %q", d.Stderr, tt.wantCauseSubstr)
				}
				if d.Command != "" {
					t.Fatalf("blocked but Command = %q, want empty", d.Command)
				}
				return
			}

			// Allow path: command must be rewritten and contain trailers.
			if d.Command == "" {
				t.Fatalf("Allow=true but Command is empty (no rewrite)\n  cmd=%q", tt.cmd)
			}
			if !strings.Contains(d.Command, trailerFlag) {
				t.Fatalf("rewrite missing %q\n  cmd=%q\n  out=%q", trailerFlag, tt.cmd, d.Command)
			}
			gotTrailers := strings.Count(d.Command, trailerKey)
			if gotTrailers != tt.wantTrailers {
				t.Errorf("trailer count = %d, want %d\n  cmd=%q\n  out=%q",
					gotTrailers, tt.wantTrailers, tt.cmd, d.Command)
			}
			for _, sub := range tt.wantPreserved {
				if !strings.Contains(d.Command, sub) {
					t.Errorf("rewrite dropped surrounding construct %q\n  cmd=%q\n  out=%q",
						sub, tt.cmd, d.Command)
				}
			}
			if tt.wantBashSyntax {
				if out, err := exec.Command("bash", "-n", "-c", d.Command).CombinedOutput(); err != nil {
					t.Errorf("bash -n syntax check failed: %v\n  cmd=%q\n  out=%q\n  bash output=%s",
						err, tt.cmd, d.Command, string(out))
				}
			}
		})
	}
}

// TestEvaluate_InertGitCommitMentionsAllowed guards against the wrapper-bypass
// fix over-blocking: commands that merely MENTION "git commit" without
// executing it — a quoted grep pattern, or a non-exec builtin like echo — must
// pass through untouched (allowed, no rewrite, no block).
func TestEvaluate_InertGitCommitMentionsAllowed(t *testing.T) {
	cfg := DefaultConfig()
	state := &State{HasRecentPass: true}

	for _, cmd := range []string{
		`grep "git commit" file.go`,
		`echo git commit`,
		`echo "run git commit later"`,
	} {
		d := Evaluate(cmd, cfg, state)
		if !d.Allow {
			t.Errorf("inert mention blocked: cmd=%q stderr=%q", cmd, d.Stderr)
		}
		if d.Command != "" {
			t.Errorf("inert mention rewritten: cmd=%q out=%q", cmd, d.Command)
		}
		if d.IsCommit {
			t.Errorf("inert mention flagged IsCommit: cmd=%q", cmd)
		}
	}
}

// TestEvaluate_ChainedCommitFlagsIsCommit proves chained/wrapped commits the
// AST path recognises are tagged IsCommit, so the caller writes a verdict for
// them (previously only IsGitCommit single-command shapes got verdicts).
func TestEvaluate_ChainedCommitFlagsIsCommit(t *testing.T) {
	cfg := DefaultConfig()
	state := &State{HasRecentPass: true}

	for _, cmd := range []string{
		`git add -A && git commit -m "msg"`,
		`make test && git commit -m "msg" && git push`,
		`GIT_AUTHOR_DATE=2026-01-01 git commit -m "msg"`,
	} {
		d := Evaluate(cmd, cfg, state)
		if !d.IsCommit {
			t.Errorf("chained commit not flagged IsCommit: cmd=%q", cmd)
		}
	}
}
