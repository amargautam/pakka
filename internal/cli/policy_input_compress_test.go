package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeInputPolicy(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".pakka"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".pakka", "policy.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// captureStderr runs fn with os.Stderr redirected and returns what was written.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	fn()
	_ = w.Close()
	os.Stderr = orig
	b, _ := io.ReadAll(r)
	return string(b)
}

// AC4: inputCompress "locked-off" suppresses auto SessionStart compression even
// when PAKKA_INPUT_COMPRESS=1 is set.
func TestInputCompress_lockedOffIgnoresEnvOptIn(t *testing.T) {
	dir := t.TempDir()
	writeInputPolicy(t, dir, `{"v":1,"inputCompress":"locked-off"}`)
	t.Setenv("PAKKA_INPUT_COMPRESS", "1")

	if maybeCompressInputFiles(dir, "sid") {
		t.Fatal("locked-off policy must suppress auto input compression despite env opt-in")
	}
}

// AC1: no policy + env opt-in → auto input compression proceeds as before.
func TestInputCompress_noPolicyRespectsEnvOptIn(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PAKKA_INPUT_COMPRESS", "1")

	if !maybeCompressInputFiles(dir, "sid") {
		t.Fatal("no policy + env opt-in must allow auto input compression")
	}
}

// AC4: the explicit /pakka:compress orchestrator run refuses with a policy
// message when input compression is locked off.
func TestInputCompress_orchestratorRunRefusesLockedOff(t *testing.T) {
	dir := t.TempDir()
	writeInputPolicy(t, dir, `{"v":1,"inputCompress":"locked-off"}`)

	out := captureStderr(t, func() {
		runOrchestrator(dir, "strict")
	})
	if !strings.Contains(out, "locked off") {
		t.Fatalf("orchestrator run must print a policy refusal message, got: %q", out)
	}
}

// A malformed policy fails closed in the orchestrator run path too.
func TestInputCompress_orchestratorRunRefusesMalformed(t *testing.T) {
	dir := t.TempDir()
	writeInputPolicy(t, dir, `{"v":1,`)

	out := captureStderr(t, func() {
		runOrchestrator(dir, "strict")
	})
	if !strings.Contains(out, "policy") {
		t.Fatalf("orchestrator run must fail closed with a policy message, got: %q", out)
	}
}
