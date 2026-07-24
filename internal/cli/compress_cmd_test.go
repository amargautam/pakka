package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// verboseContext is prose with articles/filler that ModeStrict compresses by
// well over 1%, so autoCompressContextFiles treats it as worth rewriting.
const verboseContext = `# Project Guide

This is the project guide that describes the way that the whole system works.
The purpose of this document is to explain all of the details so that a brand
new developer is able to understand exactly what is going on here very quickly.

Please always make sure that you have read this entire document before you
start to make any of the changes to the code that lives inside the repository.
`

// TestMaybeCompressInputFiles_DefaultOff_NoRewrite is a behavioral guard: with
// no opt-in, SessionStart input-file compression must NOT touch context files.
func TestMaybeCompressInputFiles_DefaultOff_NoRewrite(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("PAKKA_INPUT_COMPRESS", "") // explicitly not opted in

	path := filepath.Join(tmp, "CLAUDE.md")
	if err := os.WriteFile(path, []byte(verboseContext), 0644); err != nil {
		t.Fatal(err)
	}

	ran := maybeCompressInputFiles(tmp, "sess-test")
	if ran {
		t.Fatal("maybeCompressInputFiles ran while input compression was off (default)")
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != verboseContext {
		t.Errorf("CLAUDE.md was rewritten while input compression was off\n--- got ---\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(tmp, "CLAUDE.original.md")); err == nil {
		t.Error("backup CLAUDE.original.md was created while input compression was off")
	}
}

// TestMaybeCompressInputFiles_OptInEnv_Rewrites is a behavioral guard: with the
// env opt-in set, SessionStart input-file compression DOES rewrite context
// files in place and writes a .original.md backup.
func TestMaybeCompressInputFiles_OptInEnv_Rewrites(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("PAKKA_INPUT_COMPRESS", "1")

	path := filepath.Join(tmp, "CLAUDE.md")
	if err := os.WriteFile(path, []byte(verboseContext), 0644); err != nil {
		t.Fatal(err)
	}

	ran := maybeCompressInputFiles(tmp, "sess-test")
	if !ran {
		t.Fatal("maybeCompressInputFiles did not run while opted in via PAKKA_INPUT_COMPRESS")
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) == verboseContext {
		t.Error("CLAUDE.md was not rewritten despite opt-in")
	}
	if len(got) >= len(verboseContext) {
		t.Errorf("rewritten CLAUDE.md not smaller: %d >= %d", len(got), len(verboseContext))
	}
	backup, err := os.ReadFile(filepath.Join(tmp, "CLAUDE.original.md"))
	if err != nil {
		t.Fatalf("expected CLAUDE.original.md backup: %v", err)
	}
	if string(backup) != verboseContext {
		t.Error("backup does not preserve the original content verbatim")
	}
}

// TestIsInputEnabled_DefaultOffEnvOptIn covers the gate directly: default off,
// truthy env values opt in, falsey/garbage values stay off.
func TestIsInputEnabled_DefaultOffEnvOptIn(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	cases := []struct {
		env  string
		want bool
	}{
		{"", false},
		{"0", false},
		{"false", false},
		{"nope", false},
		{"1", true},
		{"true", true},
		{"TRUE", true},
		{"yes", true},
		{"on", true},
	}
	for _, c := range cases {
		t.Setenv("PAKKA_INPUT_COMPRESS", c.env)
		if got := isInputEnabled(); got != c.want {
			t.Errorf("isInputEnabled() with PAKKA_INPUT_COMPRESS=%q = %v; want %v", c.env, got, c.want)
		}
	}
}

func TestCompressCmdName(t *testing.T) {
	cmd := &CompressCmd{}
	if cmd.Name() != "compress" {
		t.Errorf("Name() = %q; want %q", cmd.Name(), "compress")
	}
}

func TestCompressCmdImplementsCommand(t *testing.T) {
	var _ Command = &CompressCmd{}
}

func TestCompressCmdRunNilNoPanic(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	cmd := &CompressCmd{}
	// Run with nil args and no stdin data — should not panic.
	// runCompress reads os.Args so we simulate empty subcommand args.
	_ = cmd.Run(nil)
}
