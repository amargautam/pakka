package commitgate_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/amargautam/pakka/internal/commitgate"
)

func TestScope_Has(t *testing.T) {
	scope := commitgate.Scope{"foo.go": {5: true, 6: true}}
	if !scope.Has("foo.go", 5) {
		t.Error("want Has(foo.go, 5) = true")
	}
	if scope.Has("foo.go", 7) {
		t.Error("want Has(foo.go, 7) = false")
	}
	if scope.Has("bar.go", 5) {
		t.Error("want Has(bar.go, 5) = false")
	}
}

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
	scope := commitgate.ChangedLines(diff)
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
	scope := commitgate.ChangedLines(diff)
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
	scope := commitgate.ChangedLines(diff)
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
	scope := commitgate.ChangedLines(diff)
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
	scope := commitgate.ChangedLines("")
	if len(scope) != 0 {
		t.Errorf("empty diff should yield empty scope, got %v", scope)
	}
}

// testFinding is a minimal ScopedFinding for scope filter tests.
type testFinding struct {
	File string
	Line int
	ID   string
}

func (f testFinding) GetFile() string { return f.File }
func (f testFinding) GetLine() int    { return f.Line }

// TestFilter_behaviorVariesWithStagedSet is the load-bearing assertion:
// the same finding must be DROPPED when the staged diff doesn't touch its
// line, and KEPT when the staged diff does.
func TestFilter_behaviorVariesWithStagedSet(t *testing.T) {
	preExisting := testFinding{File: "main.go", Line: 299, ID: "shell-injection"}

	// Case A: staged diff is markdown-only.
	mdOnly := `diff --git a/README.md b/README.md
--- a/README.md
+++ b/README.md
@@ -1 +1 @@
-old
+new
`
	scopeA := commitgate.ChangedLines(mdOnly)
	gotA := commitgate.Filter([]testFinding{preExisting}, scopeA)
	if len(gotA) != 0 {
		t.Fatalf("markdown-only diff: expected 0 findings, got %d (%v)", len(gotA), gotA)
	}

	// Case B: same finding, but now the diff stages main.go:299.
	stagedGo := `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -299 +299 @@ ctx
-old
+new
`
	scopeB := commitgate.ChangedLines(stagedGo)
	gotB := commitgate.Filter([]testFinding{preExisting}, scopeB)
	if len(gotB) != 1 || gotB[0].ID != "shell-injection" {
		t.Fatalf("staged-go diff: expected 1 kept finding, got %v", gotB)
	}

	if reflect.DeepEqual(scopeA, scopeB) {
		t.Fatal("scopes A and B should differ; test would be vacuous")
	}
}

func TestFilter_dropsMissingLineOrFile(t *testing.T) {
	scope := commitgate.Scope{"a.go": {1: true}}
	in := []testFinding{
		{File: "a.go", Line: 1, ID: "ok"},
		{File: "", Line: 1, ID: "no-file"},
		{File: "a.go", Line: 0, ID: "no-line"},
		{File: "a.go", Line: -1, ID: "neg-line"},
	}
	got := commitgate.Filter(in, scope)
	if len(got) != 1 || got[0].ID != "ok" {
		t.Errorf("expected only ok finding to survive, got %v", got)
	}
}

// TestFilter_commitgateFinding verifies commitgate.Finding itself satisfies
// the ScopedFinding interface used by Filter.
func TestFilter_commitgateFinding(t *testing.T) {
	scope := commitgate.Scope{"main.go": {10: true}}
	findings := []commitgate.Finding{
		{File: "main.go", Line: 10, Severity: "error", Confidence: 90},
		{File: "main.go", Line: 20, Severity: "error", Confidence: 90},
	}
	got := commitgate.Filter(findings, scope)
	if len(got) != 1 || got[0].Line != 10 {
		t.Errorf("expected only line-10 finding, got %v", got)
	}
}

// TestEndToEnd_realGitDiff drives the full pipeline through a real git repo.
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
	write("README.md", "intro\n")
	write("main.go", "package main\n\nfunc bad() { /* shell-injection */ }\n")
	run("add", ".")
	run("commit", "-q", "-m", "init")

	write("README.md", "intro updated\n")
	run("add", "README.md")

	cmd := exec.Command("git", "diff", "--cached", "--unified=0")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git diff: %v", err)
	}
	scope := commitgate.ChangedLines(string(out))

	preExisting := []testFinding{{File: "main.go", Line: 3, ID: "bad-pattern"}}
	kept := commitgate.Filter(preExisting, scope)
	if len(kept) != 0 {
		t.Fatalf("pre-existing main.go finding must be filtered out for md-only commit, got %v", kept)
	}

	write("main.go", "package main\n\nfunc bad() { /* shell-injection edited */ }\n")
	run("add", "main.go")

	cmd = exec.Command("git", "diff", "--cached", "--unified=0")
	cmd.Dir = dir
	out, err = cmd.Output()
	if err != nil {
		t.Fatalf("git diff (2): %v", err)
	}
	scope2 := commitgate.ChangedLines(string(out))
	kept2 := commitgate.Filter(preExisting, scope2)
	if len(kept2) != 1 {
		t.Fatalf("after staging main.go, finding must survive filter, got %v (scope=%v)", kept2, scope2)
	}

	if reflect.DeepEqual(scope, scope2) {
		t.Fatal("scopes before/after staging main.go should differ")
	}
}
