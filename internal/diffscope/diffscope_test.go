package diffscope_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/amargautam/pakka/internal/diffscope"
)

// finding is a minimal Finding implementation for tests.
type finding struct {
	File string
	Line int
	ID   string // arbitrary tag so we can assert which findings survived
}

func (f finding) GetFile() string { return f.File }
func (f finding) GetLine() int    { return f.Line }

func TestChangedLines_simpleHunks(t *testing.T) {
	diff := `diff --git a/foo.go b/foo.go
index 0000000..1111111 100644
--- a/foo.go
+++ b/foo.go
@@ -2 +2 @@ a
-b
+B
@@ -5,0 +6 @@ e
+f
`
	scope := diffscope.ChangedLines(diff)
	if !scope.Has("foo.go", 2) {
		t.Errorf("expected foo.go:2 in scope")
	}
	if !scope.Has("foo.go", 6) {
		t.Errorf("expected foo.go:6 in scope")
	}
	for _, line := range []int{1, 3, 4, 5, 7, 100} {
		if scope.Has("foo.go", line) {
			t.Errorf("foo.go:%d should NOT be in scope", line)
		}
	}
}

func TestChangedLines_multiFile(t *testing.T) {
	diff := `diff --git a/README.md b/README.md
new file mode 100644
--- /dev/null
+++ b/README.md
@@ -0,0 +1 @@
+# hi
diff --git a/foo.go b/foo.go
index 9405325..91ac79b 100644
--- a/foo.go
+++ b/foo.go
@@ -2 +2 @@ a
-b
+B
`
	scope := diffscope.ChangedLines(diff)
	if !scope.Has("README.md", 1) {
		t.Error("README.md:1 should be in scope")
	}
	if !scope.Has("foo.go", 2) {
		t.Error("foo.go:2 should be in scope")
	}
	if scope.Has("foo.go", 1) || scope.Has("README.md", 2) {
		t.Error("untouched lines must not be in scope")
	}
	gotFiles := scope.Files()
	wantFiles := []string{"README.md", "foo.go"}
	if !reflect.DeepEqual(gotFiles, wantFiles) {
		t.Errorf("Files() = %v, want %v", gotFiles, wantFiles)
	}
}

func TestChangedLines_pureDeletionContributesNoLines(t *testing.T) {
	diff := `diff --git a/dead.go b/dead.go
deleted file mode 100644
--- a/dead.go
+++ /dev/null
@@ -1,3 +0,0 @@
-x
-y
-z
`
	scope := diffscope.ChangedLines(diff)
	if len(scope) != 0 {
		t.Errorf("pure deletion should yield empty scope, got %v", scope)
	}
}

func TestChangedLines_multiLineHunk(t *testing.T) {
	diff := `diff --git a/x b/x
--- a/x
+++ b/x
@@ -1,0 +10,3 @@
+line10
+line11
+line12
`
	scope := diffscope.ChangedLines(diff)
	for _, line := range []int{10, 11, 12} {
		if !scope.Has("x", line) {
			t.Errorf("x:%d should be in scope", line)
		}
	}
	for _, line := range []int{9, 13} {
		if scope.Has("x", line) {
			t.Errorf("x:%d should NOT be in scope", line)
		}
	}
}

func TestChangedLines_emptyDiff(t *testing.T) {
	scope := diffscope.ChangedLines("")
	if len(scope) != 0 {
		t.Errorf("empty diff should yield empty scope, got %v", scope)
	}
}

// TestFilter_behaviorVariesWithStagedSet is the load-bearing assertion for
// the gate-scoping fix: the same finding must be DROPPED when the staged
// diff doesn't touch its line, and KEPT when the staged diff does.
//
// This is the test required by ~/.claude/projects/-Users-amar-Projects-pakka-dev/
// memory/feedback_measurement_first.md — measurements must vary with input.
func TestFilter_behaviorVariesWithStagedSet(t *testing.T) {
	preExisting := finding{File: "main.go", Line: 299, ID: "shell-injection"}

	// Case A: staged diff is markdown-only. Pre-existing Go finding is
	// out-of-scope and MUST be dropped.
	mdOnly := `diff --git a/README.md b/README.md
--- a/README.md
+++ b/README.md
@@ -1 +1 @@
-old
+new
`
	scopeA := diffscope.ChangedLines(mdOnly)
	gotA := diffscope.Filter([]finding{preExisting}, scopeA)
	if len(gotA) != 0 {
		t.Fatalf("markdown-only diff: expected 0 findings, got %d (%v)", len(gotA), gotA)
	}

	// Case B: same finding, but now the diff stages main.go:299. Finding
	// MUST be kept. Same finding, different scope, different verdict —
	// proves the filter is doing real work.
	stagedGo := `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -299 +299 @@ ctx
-old
+new
`
	scopeB := diffscope.ChangedLines(stagedGo)
	gotB := diffscope.Filter([]finding{preExisting}, scopeB)
	if len(gotB) != 1 || gotB[0].ID != "shell-injection" {
		t.Fatalf("staged-go diff: expected 1 kept finding, got %v", gotB)
	}

	// Sanity: the two scopes must actually differ. If both ended up the
	// same, the test would pass trivially without exercising the filter.
	if reflect.DeepEqual(scopeA, scopeB) {
		t.Fatal("scopes A and B should differ; test would be vacuous")
	}
}

func TestFilter_dropsMissingLineOrFile(t *testing.T) {
	scope := diffscope.Scope{"a.go": {1: true}}
	in := []finding{
		{File: "a.go", Line: 1, ID: "ok"},
		{File: "", Line: 1, ID: "no-file"},
		{File: "a.go", Line: 0, ID: "no-line"},
		{File: "a.go", Line: -1, ID: "neg-line"},
	}
	got := diffscope.Filter(in, scope)
	if len(got) != 1 || got[0].ID != "ok" {
		t.Errorf("expected only ok finding to survive, got %v", got)
	}
}

// TestEndToEnd_realGitDiff drives the full pipeline through a real git
// repo: stage a markdown change, plant an unrelated Go file, run
// `git diff --cached --unified=0`, parse it, and confirm a Go finding on
// an unstaged line is filtered out. This is the regression test for the
// "markdown commit blocked by main.go:299" bug.
func TestEndToEnd_realGitDiff(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	run("init", "-q")
	run("config", "user.email", "t@t.t")
	run("config", "user.name", "t")
	// Initial commit: README + a Go file with a known-bad pattern at line 3.
	write("README.md", "intro\n")
	write("main.go", "package main\n\nfunc bad() { /* shell-injection */ }\n")
	run("add", ".")
	run("commit", "-q", "-m", "init")

	// Stage a markdown-only change. Do NOT stage anything in main.go.
	write("README.md", "intro updated\n")
	run("add", "README.md")

	cmd := exec.Command("git", "diff", "--cached", "--unified=0")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git diff: %v", err)
	}
	scope := diffscope.ChangedLines(string(out))

	// Plant a finding on the pre-existing main.go:3 (the "bad pattern").
	preExisting := []finding{{File: "main.go", Line: 3, ID: "bad-pattern"}}
	kept := diffscope.Filter(preExisting, scope)
	if len(kept) != 0 {
		t.Fatalf("pre-existing main.go finding must be filtered out for md-only commit, got %v", kept)
	}

	// Now stage main.go too. Same finding; same line; different scope.
	write("main.go", "package main\n\nfunc bad() { /* shell-injection edited */ }\n")
	run("add", "main.go")

	cmd = exec.Command("git", "diff", "--cached", "--unified=0")
	cmd.Dir = dir
	out, err = cmd.Output()
	if err != nil {
		t.Fatalf("git diff (2): %v", err)
	}
	scope2 := diffscope.ChangedLines(string(out))
	kept2 := diffscope.Filter(preExisting, scope2)
	if len(kept2) != 1 {
		t.Fatalf("after staging main.go, finding must survive filter, got %v (scope=%v)", kept2, scope2)
	}

	// Behavior assertion: the verdict varies with the staged set.
	if reflect.DeepEqual(scope, scope2) {
		t.Fatal("scopes before/after staging main.go should differ")
	}
}
