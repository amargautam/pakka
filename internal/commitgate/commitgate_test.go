package commitgate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestEvaluate(t *testing.T) {
	cfg := DefaultConfig()

	passState := &State{HasRecentPass: true}
	noPassState := &State{}

	tests := []struct {
		name          string
		cmd           string
		cfg           *Config
		state         *State
		wantAllow     bool
		wantSubstr    string // substring expected in rewritten Command
		wantNoRewrite bool   // Command must be empty (no-op)
	}{
		{
			name:       "plain git commit with recent pass → strong trailer + co-author",
			cmd:        `git commit -m "x"`,
			cfg:        cfg,
			state:      passState,
			wantAllow:  true,
			wantSubstr: "(gate: passed)",
		},
		{
			name:       "git commit editor mode → rewritten",
			cmd:        `git commit`,
			cfg:        cfg,
			state:      passState,
			wantAllow:  true,
			wantSubstr: "(gate: passed)",
		},
		{
			name:       "git commit --amend → rewritten",
			cmd:        `git commit --amend`,
			cfg:        cfg,
			state:      passState,
			wantAllow:  true,
			wantSubstr: "(gate: passed)",
		},
		{
			name:          "already has both trailers → no-op",
			cmd:           `git commit -m "x" --trailer "Reviewed-by-pakka: v0.1.0" --trailer "Co-authored-by: pakka <279024857+pakka-bot@users.noreply.github.com>"`,
			cfg:           cfg,
			state:         passState,
			wantAllow:     true,
			wantNoRewrite: true,
		},
		{
			name:          "both trailers in message body → no-op",
			cmd:           "git commit -m \"x\n\nReviewed-by-pakka: v0.1.0\nCo-authored-by: pakka <279024857+pakka-bot@users.noreply.github.com>\"",
			cfg:           cfg,
			state:         passState,
			wantAllow:     true,
			wantNoRewrite: true,
		},
		{
			name:          "git log (not a commit) → no-op",
			cmd:           `git log`,
			cfg:           cfg,
			state:         passState,
			wantAllow:     true,
			wantNoRewrite: true,
		},
		{
			name:          "[skip pakka] → no trailers",
			cmd:           `git commit -m "[skip pakka] quick fix"`,
			cfg:           cfg,
			state:         noPassState,
			wantAllow:     true,
			wantNoRewrite: true,
		},
		{
			name:          "signature=false AND coAuthor=false → no-op",
			cmd:           `git commit -m "x"`,
			cfg:           &Config{Signature: false, CoAuthor: false, AutoGate: false, Version: "0.1.0"},
			state:         noPassState,
			wantAllow:     true,
			wantNoRewrite: true,
		},
		{
			name:       "autoGate=false → baseline trailer",
			cmd:        `git commit -m "x"`,
			cfg:        &Config{Signature: true, CoAuthor: true, AutoGate: false, Version: "0.1.0"},
			state:      noPassState,
			wantAllow:  true,
			wantSubstr: "Reviewed-by-pakka: v0.1.0",
		},
		{
			name: "review fails → exit 2, stderr has finding",
			cmd:  `git commit -m "x"`,
			cfg:  cfg,
			state: &State{ErrorFindings: []Finding{
				{File: "main.go", Line: 42, Severity: "error", Confidence: 95, Rationale: "nil deref", Fix: "add nil check"},
			}},
			wantAllow: false,
		},
		{
			name:       "review passes → strong trailer",
			cmd:        `git commit -m "feat: add X"`,
			cfg:        cfg,
			state:      passState,
			wantAllow:  true,
			wantSubstr: "(gate: passed)",
		},
		// Finding 1: gate decoupled from Signature.
		{
			name:          "gate: sig=false coAuthor=false autoGate=true clean → allow, no rewrite",
			cmd:           `git commit -m "x"`,
			cfg:           &Config{Signature: false, CoAuthor: false, AutoGate: true, Version: "0.1.0"},
			state:         passState,
			wantAllow:     true,
			wantNoRewrite: true,
		},
		{
			name:      "gate: sig=false coAuthor=false autoGate=true errors → block",
			cmd:       `git commit -m "x"`,
			cfg:       &Config{Signature: false, CoAuthor: false, AutoGate: true, Version: "0.1.0"},
			state:     &State{ErrorFindings: []Finding{{File: "a.go", Line: 1, Severity: "error", Confidence: 90, Rationale: "bug"}}},
			wantAllow: false,
		},
		{
			name:       "gate: sig=false coAuthor=true autoGate=true clean → only Trailer B",
			cmd:        `git commit -m "x"`,
			cfg:        &Config{Signature: false, CoAuthor: true, AutoGate: true, Version: "0.1.0"},
			state:      passState,
			wantAllow:  true,
			wantSubstr: coAuthorPakkaEmail,
		},
		{
			name:      "gate: sig=true coAuthor=false autoGate=true errors → block",
			cmd:       `git commit -m "x"`,
			cfg:       &Config{Signature: true, CoAuthor: false, AutoGate: true, Version: "0.1.0"},
			state:     &State{ErrorFindings: []Finding{{File: "b.go", Line: 2, Severity: "error", Confidence: 85, Rationale: "oops"}}},
			wantAllow: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := Evaluate(tt.cmd, tt.cfg, tt.state)

			if d.Allow != tt.wantAllow {
				t.Fatalf("Allow = %v, want %v", d.Allow, tt.wantAllow)
			}

			if tt.wantNoRewrite && d.Command != "" {
				t.Fatalf("Command = %q, want empty (no-op)", d.Command)
			}

			if tt.wantSubstr != "" {
				if d.Command == "" {
					t.Fatalf("Command is empty, want substring %q", tt.wantSubstr)
				}
				if !strings.Contains(d.Command, tt.wantSubstr) {
					t.Fatalf("Command = %q, missing %q", d.Command, tt.wantSubstr)
				}
			}

			if !tt.wantAllow {
				if d.Stderr == "" {
					t.Fatal("expected stderr on block, got empty")
				}
				if d.Command != "" {
					t.Fatalf("blocked but Command = %q, want empty", d.Command)
				}
			}
		})
	}
}

func TestDualTrailer(t *testing.T) {
	coAuthor := CoAuthorTrailer()

	tests := []struct {
		name       string
		cmd        string
		cfg        *Config
		state      *State
		wantA      bool // expect Trailer A in Command
		wantB      bool // expect Trailer B in Command
		wantNoOp   bool // Command must be empty
		wantAllow  bool
	}{
		{
			name:      "clean commit → both trailers",
			cmd:       `git commit -m "feat: add X"`,
			cfg:       DefaultConfig(),
			state:     &State{HasRecentPass: true},
			wantA:     true,
			wantB:     true,
			wantAllow: true,
		},
		{
			name:      "already has Reviewed-by-pakka → add only Co-authored-by",
			cmd:       `git commit -m "x" --trailer "Reviewed-by-pakka: v0.1.0 (gate: passed)"`,
			cfg:       DefaultConfig(),
			state:     &State{HasRecentPass: true},
			wantA:     false,
			wantB:     true,
			wantAllow: true,
		},
		{
			name:      "already has Co-authored-by pakka → add only Reviewed-by-pakka",
			cmd:       `git commit -m "x" --trailer "` + coAuthor + `"`,
			cfg:       DefaultConfig(),
			state:     &State{HasRecentPass: true},
			wantA:     true,
			wantB:     false,
			wantAllow: true,
		},
		{
			name:      "already has both → no-op",
			cmd:       `git commit -m "x" --trailer "Reviewed-by-pakka: v0.1.0" --trailer "` + coAuthor + `"`,
			cfg:       DefaultConfig(),
			state:     &State{HasRecentPass: true},
			wantNoOp:  true,
			wantAllow: true,
		},
		{
			name:      "coAuthor=false, signature=true → only Trailer A",
			cmd:       `git commit -m "x"`,
			cfg:       &Config{Signature: true, CoAuthor: false, AutoGate: true, Version: "0.1.0"},
			state:     &State{HasRecentPass: true},
			wantA:     true,
			wantB:     false,
			wantAllow: true,
		},
		{
			name:      "coAuthor=true, signature=false → only Trailer B",
			cmd:       `git commit -m "x"`,
			cfg:       &Config{Signature: false, CoAuthor: true, AutoGate: true, Version: "0.1.0"},
			state:     &State{HasRecentPass: true},
			wantA:     false,
			wantB:     true,
			wantAllow: true,
		},
		{
			name:      "both false → full no-op",
			cmd:       `git commit -m "x"`,
			cfg:       &Config{Signature: false, CoAuthor: false, AutoGate: true, Version: "0.1.0"},
			state:     &State{HasRecentPass: true},
			wantNoOp:  true,
			wantAllow: true,
		},
		{
			name:      "[skip pakka] → skip BOTH regardless of settings",
			cmd:       `git commit -m "[skip pakka] quick fix"`,
			cfg:       DefaultConfig(),
			state:     &State{HasRecentPass: true},
			wantNoOp:  true,
			wantAllow: true,
		},
		{
			name:      "Co-Authored-By: Claude present → does NOT suppress pakka Co-authored-by",
			cmd:       `git commit -m "feat: X" --trailer "Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"`,
			cfg:       DefaultConfig(),
			state:     &State{HasRecentPass: true},
			wantA:     true,
			wantB:     true,
			wantAllow: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := Evaluate(tt.cmd, tt.cfg, tt.state)

			if d.Allow != tt.wantAllow {
				t.Fatalf("Allow = %v, want %v", d.Allow, tt.wantAllow)
			}

			if tt.wantNoOp {
				if d.Command != "" {
					t.Fatalf("expected no-op, got Command = %q", d.Command)
				}
				return
			}

			if d.Command == "" {
				t.Fatal("expected rewritten command, got empty")
			}

			// Count new trailer additions (subtract what was already in the input).
			addedA := strings.Count(d.Command, "Reviewed-by-pakka:") - strings.Count(tt.cmd, "Reviewed-by-pakka:")
			addedB := strings.Count(d.Command, coAuthorPakkaEmail) - strings.Count(tt.cmd, coAuthorPakkaEmail)

			if tt.wantA && addedA != 1 {
				t.Errorf("expected 1 new Trailer A, got %d additions in %q", addedA, d.Command)
			}
			if !tt.wantA && addedA != 0 {
				t.Errorf("expected 0 new Trailer A, got %d additions in %q", addedA, d.Command)
			}
			if tt.wantB && addedB != 1 {
				t.Errorf("expected 1 new Trailer B, got %d additions in %q", addedB, d.Command)
			}
			if !tt.wantB && addedB != 0 {
				t.Errorf("expected 0 new Trailer B, got %d additions in %q", addedB, d.Command)
			}
		})
	}
}

func TestSkipPakkaNoTrailers(t *testing.T) {
	d := Evaluate(`git commit -m "[skip pakka] quick fix"`, DefaultConfig(), &State{})
	if d.Command != "" {
		t.Errorf("[skip pakka] must not produce any trailers, got Command = %q", d.Command)
	}
	if d.AuditNote != "review_skipped=user_skip" {
		t.Errorf("AuditNote = %q, want review_skipped=user_skip", d.AuditNote)
	}
}

func TestAutoGateFalseNoStrongTrailer(t *testing.T) {
	cfg := &Config{Signature: true, CoAuthor: true, AutoGate: false, Version: "0.1.0"}
	d := Evaluate(`git commit -m "x"`, cfg, &State{})
	if strings.Contains(d.Command, "(gate: passed)") {
		t.Error("autoGate=false must not produce strong trailer")
	}
}

func TestIsGitCommitEdgeCases(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		{"git commit", true},
		{"git commit -m 'x'", true},
		{"git commit --amend", true},
		{"  git commit -m 'x'", true},
		{"\tgit commit", true},
		{"git commit-graph", false},
		{"git log", false},
		{"echo git commit", false},
		{"git commitall", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsGitCommit(tt.cmd); got != tt.want {
			t.Errorf("IsGitCommit(%q) = %v, want %v", tt.cmd, got, tt.want)
		}
	}
}

func TestOversizeDiffSkipsGate(t *testing.T) {
	cfg := DefaultConfig()
	state := &State{DiffBytes: 300000}
	d := Evaluate(`git commit -m "big change"`, cfg, state)
	if !d.Allow {
		t.Fatal("oversize diff should allow")
	}
	if d.AuditNote != "review_skipped=oversize" {
		t.Errorf("AuditNote = %q, want review_skipped=oversize", d.AuditNote)
	}
	if !strings.Contains(d.Command, "Reviewed-by-pakka:") {
		t.Error("oversize should still get baseline trailer")
	}
	if strings.Contains(d.Command, "(gate: passed)") {
		t.Error("oversize should not get strong trailer")
	}
	if !strings.Contains(d.Command, coAuthorPakkaEmail) {
		t.Error("oversize should still get Co-authored-by trailer")
	}
}

func TestFormatFindings(t *testing.T) {
	findings := []Finding{
		{File: "main.go", Line: 42, Severity: "error", Confidence: 95, Rationale: "nil deref", Fix: "add nil check"},
	}
	out := FormatFindings(findings)
	if !strings.Contains(out, "1 error(s)") {
		t.Errorf("missing error count in %q", out)
	}
	if !strings.Contains(out, "main.go:42") {
		t.Errorf("missing file:line in %q", out)
	}
	if !strings.Contains(out, "nil deref") {
		t.Errorf("missing rationale in %q", out)
	}
	if !strings.Contains(out, "fix: add nil check") {
		t.Errorf("missing fix in %q", out)
	}
}

func TestCoAuthorTrailer(t *testing.T) {
	got := CoAuthorTrailer()
	want := "Co-authored-by: pakka <279024857+pakka-bot@users.noreply.github.com>"
	if got != want {
		t.Errorf("CoAuthorTrailer() = %q, want %q", got, want)
	}
}

// --- Finding 4: tightened matchers ---

func TestSkipMarkerInProse(t *testing.T) {
	// [skip pakka] mentioned in prose → NOT a skip.
	cmd := `git commit -m "docs: explain how [skip pakka] marker works"`
	if HasSkipMarker(cmd) {
		t.Error("prose mention of [skip pakka] should not trigger skip")
	}
	// Verify gate still runs and trailer is injected.
	d := Evaluate(cmd, DefaultConfig(), &State{HasRecentPass: true})
	if d.Command == "" {
		t.Error("expected trailer injection, got no-op")
	}
}

func TestSkipMarkerPositions(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		want bool
	}{
		{"start of message", `git commit -m "[skip pakka] quick fix"`, true},
		{"end of message", `git commit -m "quick fix [skip pakka]"`, true},
		{"own line", "git commit -m \"fix\n\n[skip pakka]\"", true},
		{"in prose", `git commit -m "docs: the [skip pakka] feature is great"`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasSkipMarker(tt.cmd); got != tt.want {
				t.Errorf("HasSkipMarker = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTrailerInProseNotSuppressed(t *testing.T) {
	// Message that DISCUSSES Reviewed-by-pakka: in prose → Trailer A still added.
	cmd := `git commit -m "docs: explain the Reviewed-by-pakka: trailer format"`
	if HasTrailerA(cmd) {
		t.Error("prose mention should not suppress Trailer A")
	}
	d := Evaluate(cmd, DefaultConfig(), &State{HasRecentPass: true})
	if !strings.Contains(d.Command, "Reviewed-by-pakka:") {
		t.Error("expected Trailer A injection despite prose mention")
	}
}

func TestTrailerInBodySuppressed(t *testing.T) {
	// Actual trailer in message body (after blank line) → suppressed.
	cmd := "git commit -m \"feat: add validation\n\nReviewed-by-pakka: v0.1.0\""
	if !HasTrailerA(cmd) {
		t.Error("actual trailer in body should be detected")
	}
}

func TestTrailerFlagSuppressed(t *testing.T) {
	// --trailer "Reviewed-by-pakka: ..." already present → suppressed.
	cmd := `git commit -m "x" --trailer "Reviewed-by-pakka: v0.1.0 (gate: passed)"`
	if !HasTrailerA(cmd) {
		t.Error("--trailer flag should be detected")
	}
}

func TestParseGitCommitArgs(t *testing.T) {
	tests := []struct {
		name         string
		cmd          string
		wantMsg      string
		wantTrailers []string
	}{
		{
			name:    "simple -m",
			cmd:     `git commit -m "hello world"`,
			wantMsg: "hello world",
		},
		{
			name:    "single-quoted -m",
			cmd:     `git commit -m 'hello world'`,
			wantMsg: "hello world",
		},
		{
			name:         "--trailer flag",
			cmd:          `git commit -m "x" --trailer "Key: Value"`,
			wantMsg:      "x",
			wantTrailers: []string{"Key: Value"},
		},
		{
			name:         "--trailer= with equals",
			cmd:          `git commit -m "x" --trailer="Key: Value"`,
			wantMsg:      "x",
			wantTrailers: []string{"Key: Value"},
		},
		{
			name:    "heredoc message",
			cmd:     "git commit -m \"$(cat <<'EOF'\nhello world\nEOF\n)\"",
			wantMsg: "hello world",
		},
		{
			name:    "heredoc with trailers",
			cmd:     "git commit -m \"$(cat <<'EOF'\nfeat: add X\n\nReviewed-by-pakka: v0.1.0\nEOF\n)\"",
			wantMsg: "feat: add X\n\nReviewed-by-pakka: v0.1.0",
		},
		{
			name:    "--message long form",
			cmd:     `git commit --message "hello"`,
			wantMsg: "hello",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parts := parseGitCommitArgs(tt.cmd)
			if parts.Message != tt.wantMsg {
				t.Errorf("Message = %q, want %q", parts.Message, tt.wantMsg)
			}
			if len(tt.wantTrailers) > 0 {
				if len(parts.Trailers) != len(tt.wantTrailers) {
					t.Fatalf("Trailers count = %d, want %d", len(parts.Trailers), len(tt.wantTrailers))
				}
				for i, want := range tt.wantTrailers {
					if parts.Trailers[i] != want {
						t.Errorf("Trailers[%d] = %q, want %q", i, parts.Trailers[i], want)
					}
				}
			}
		})
	}
}

// --- Shell-injection tests for InjectTrailer ---

// stubGitDir creates a temp dir containing a `git` shim that writes its argv
// (one arg per line) to argvFile. It returns the dir path so tests can prepend
// it to PATH. Assertions are performed by inspecting argvFile contents.
func stubGitDir(t *testing.T, argvFile string) string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\" >> " + shellQuote(argvFile) + "\ndone\nexit 0\n"
	path := filepath.Join(dir, "git")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub git: %v", err)
	}
	return dir
}

// runUnderShell executes shellCmd via `sh -c`, with PATH set so that the stub
// `git` from gitDir is found first. Returns combined output (rarely useful;
// tests assert via side-effect files).
func runUnderShell(t *testing.T, gitDir, shellCmd string) {
	t.Helper()
	cmd := exec.Command("sh", "-c", shellCmd)
	cmd.Env = append(os.Environ(), "PATH="+gitDir+":"+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("sh -c output: %s", string(out))
		t.Fatalf("sh -c failed: %v", err)
	}
}

// TestInjectTrailer_quoteInjection plants an attack payload in the trailer
// value that, if not properly escaped, would `touch` a sentinel file via the
// shell. After running the rewritten command through `sh -c`, the sentinel
// must NOT exist — proving the trailer was treated as a literal argument.
func TestInjectTrailer_quoteInjection(t *testing.T) {
	tmp := t.TempDir()
	sentinel := filepath.Join(tmp, "pwn-sentinel")
	argvFile := filepath.Join(tmp, "argv.log")
	gitDir := stubGitDir(t, argvFile)

	// Classic shell-injection payloads. Each tries to break out of the quoted
	// trailer arg and execute `touch <sentinel>` as a side-effect command.
	payloads := []string{
		`"; touch ` + sentinel + `; #`,
		`'; touch ` + sentinel + `; #`,
		"$(touch " + sentinel + ")",
		"`touch " + sentinel + "`",
		`\"; touch ` + sentinel + `; #`,
	}
	for _, p := range payloads {
		t.Run("payload="+p, func(t *testing.T) {
			_ = os.Remove(sentinel)
			_ = os.Remove(argvFile)

			rewritten := InjectTrailer(`git commit -m "x"`, p)
			runUnderShell(t, gitDir, rewritten)

			if _, err := os.Stat(sentinel); err == nil {
				t.Fatalf("shell injection succeeded: sentinel %s exists; rewritten=%q", sentinel, rewritten)
			}

			// Also confirm the trailer reached git literally as a single arg.
			data, err := os.ReadFile(argvFile)
			if err != nil {
				t.Fatalf("stub git did not run: %v", err)
			}
			if !strings.Contains(string(data), p) {
				t.Fatalf("trailer %q not present verbatim in argv:\n%s", p, string(data))
			}
		})
	}
}

// TestInjectTrailer_specialChars verifies trailers containing shell-meaningful
// characters round-trip as a single literal argument when the result is parsed
// by /bin/sh.
func TestInjectTrailer_specialChars(t *testing.T) {
	tmp := t.TempDir()
	argvFile := filepath.Join(tmp, "argv.log")
	gitDir := stubGitDir(t, argvFile)

	cases := []struct {
		name    string
		trailer string
	}{
		{"double-quote", `He said "hi"`},
		{"single-quote", `it's fine`},
		{"dollar-var", `value=$HOME and $PATH`},
		{"backtick", "ts=`date`"},
		{"backslash", `a\b\c`},
		{"newline", "line1\nline2"},
		{"mixed", `mix: "x" 'y' $z \w ` + "`q`"},
		{"empty", ``},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_ = os.Remove(argvFile)
			rewritten := InjectTrailer(`git commit -m "x"`, tc.trailer)
			runUnderShell(t, gitDir, rewritten)

			data, err := os.ReadFile(argvFile)
			if err != nil {
				t.Fatalf("stub git did not run: %v (rewritten=%q)", err, rewritten)
			}
			// Stub git writes one arg per line. Strip exactly one trailing newline
			// (printf '%s\n' always appends one); preserve trailing empty-arg lines.
			s := string(data)
			if strings.HasSuffix(s, "\n") {
				s = s[:len(s)-1]
			}
			args := strings.Split(s, "\n")
			var got string
			found := false
			for i, a := range args {
				if a == "--trailer" && i+1 < len(args) {
					got = args[i+1]
					// Re-stitch any embedded newlines: the trailer value may
					// have been split across multiple lines by the shim.
					for j := i + 2; j < len(args); j++ {
						got += "\n" + args[j]
					}
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("--trailer arg not seen in argv:\n%s", string(data))
			}
			if got != tc.trailer {
				t.Fatalf("trailer round-trip mismatch:\n  want %q\n   got %q\n  rewritten=%q", tc.trailer, got, rewritten)
			}
		})
	}
}

// TestInjectTrailer_behaviorVariesWithInput proves the test harness actually
// observes per-input behavior — not a stub that ignores its argument. Two
// distinct trailer inputs must produce two distinct observed git invocations.
// (Per memory: feedback_measurement_first.md.)
func TestInjectTrailer_behaviorVariesWithInput(t *testing.T) {
	tmp := t.TempDir()
	gitDir := stubGitDir(t, "/dev/null") // discard; we use a fresh argvFile per run

	run := func(trailer string) string {
		argvFile := filepath.Join(tmp, fmt.Sprintf("argv-%x.log", []byte(trailer)))
		_ = os.Remove(argvFile)
		// Re-make the stub with this argvFile.
		gitDir2 := stubGitDir(t, argvFile)
		rewritten := InjectTrailer(`git commit -m "x"`, trailer)
		runUnderShell(t, gitDir2, rewritten)
		data, err := os.ReadFile(argvFile)
		if err != nil {
			t.Fatalf("argv read: %v", err)
		}
		return string(data)
	}

	a := run("foo")
	b := run("bar")

	if a == b {
		t.Fatalf("expected different observed argv for different inputs, got identical:\n%s", a)
	}
	if !strings.Contains(a, "foo") {
		t.Fatalf("argv for trailer=foo missing literal 'foo':\n%s", a)
	}
	if !strings.Contains(b, "bar") {
		t.Fatalf("argv for trailer=bar missing literal 'bar':\n%s", b)
	}
	// Sanity: the unused gitDir is harmless — referenced to avoid unused-var
	// complaints if the test is ever simplified.
	_ = gitDir
}

// TestShellQuote_unitProperties checks the quoting primitive directly.
// Empty input yields '' (valid empty sh string). Embedded single quote is
// escaped via the standard '\'' sequence.
func TestShellQuote_unitProperties(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", "''"},
		{"abc", "'abc'"},
		{"a'b", `'a'\''b'`},
		{`'`, `''\'''`},
		{`$x"y` + "`z`", `'$x"y` + "`z`" + `'`},
	}
	for _, c := range cases {
		got := shellQuote(c.in)
		if got != c.want {
			t.Errorf("shellQuote(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func BenchmarkEvaluateRewrite(b *testing.B) {
	cfg := DefaultConfig()
	state := &State{HasRecentPass: true}
	cmd := `git commit -m "feat: add validation"`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Evaluate(cmd, cfg, state)
	}
}

func BenchmarkEvaluateNoOp(b *testing.B) {
	cfg := DefaultConfig()
	state := &State{HasRecentPass: true}
	cmd := `git log --oneline`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Evaluate(cmd, cfg, state)
	}
}
