package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/amargautam/pakka/internal/compress/semantic"
	"github.com/amargautam/pakka/internal/meter"
)

// DefaultTargets is the allowlist of memory-files the orchestrator scans
// when no override is supplied via settings.json.
var DefaultTargets = []string{
	"CLAUDE.md",
	"DESIGN.md",
	"BUILD.md",
	"memory/LOG.md",
	"memory/DECISIONS.md",
}

// Orchestrator coordinates one walk over the allowlist.
//
// Run is synchronous; RunAsync forks a detached background process.
// LogWriter (when non-nil) receives one line per file processed; tests
// inject a buffer here. Production wires it to ~/.pakka/orchestrator.log.
type Orchestrator struct {
	Repo      string
	Targets   []string
	Level     string
	SessionID string
	Rewriter  semantic.Rewriter
	LogWriter io.Writer
	// Now is injectable for tests; defaults to time.Now when nil.
	Now func() time.Time
	// stateLock guards state writes when multiple goroutines call Run.
	stateLock sync.Mutex
	// defaultLog is the cached writer for the production orchestrator log.
	// Opened lazily on first logf when LogWriter is nil; closed by Run on
	// completion. Caching avoids the per-call open/leak that bloated FDs in
	// long-running orchestrator-bg processes.
	logOnce    sync.Once
	defaultLog *os.File
}

// Run walks the allowlist once. Eligible+stale files are recompressed; up-to-date
// files are skipped; locked files are skipped with a log line. Errors per file
// never fail the whole walk — orchestrator must always return nil unless the
// state save itself fails.
//
// Purpose: Single pass of the auto-orchestrator.
// Errors: Returns the state-save error, if any. Per-file failures are logged.
func (o *Orchestrator) Run(ctx context.Context) error {
	if o.Repo == "" {
		return errors.New("orchestrator: empty repo")
	}
	now := o.Now
	if now == nil {
		now = time.Now
	}
	level := o.Level
	if level == "" {
		// "ultra" is pakka's brand default — see memory/DECISIONS.md
		// "Default output level: ultra (decided 2026-04-29)". An Orchestrator
		// constructed without an explicit Level falls back to ultra so the
		// auto-orchestrator stays consistent with loadOutputLevel().
		level = "ultra"
	}
	targets := o.Targets
	if len(targets) == 0 {
		targets = DefaultTargets
	}

	defer o.closeDefaultLog()

	state, err := LoadState(o.Repo)
	if err != nil {
		o.logf("state: load failed: %v", err)
		state = NewState()
	}

	for _, rel := range targets {
		o.processOne(ctx, rel, level, state, now)
	}

	return state.Save(o.Repo)
}

// RunAsync forks a detached `pakka-core compress --orchestrator-bg` process
// using the currently-running executable. The child writes to
// ~/.pakka/orchestrator.log, has stdin closed, and survives the parent's exit
// via Setsid (POSIX) or CREATE_NEW_PROCESS_GROUP (Windows, set by syscall.go).
//
// Purpose: Keep SessionStart hook return time under 50ms.
// Errors: None reported back; failures are written to the orchestrator log.
func (o *Orchestrator) RunAsync() {
	cmd := o.AsyncCommand()
	if cmd == nil {
		o.logf("async: cannot construct command — current executable unavailable")
		return
	}
	if err := cmd.Start(); err != nil {
		o.logf("async: start failed: %v", err)
		return
	}
	// Detach: do not Wait. The child is now in its own process group.
	go func() {
		_ = cmd.Process.Release()
	}()
}

// AsyncCommand builds (but does not start) the *exec.Cmd that RunAsync would
// invoke. Exposed for table-tests asserting argument shape without forking.
func (o *Orchestrator) AsyncCommand() *exec.Cmd {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		return nil
	}
	args := []string{
		"compress",
		"--orchestrator-bg",
		"--level=" + o.Level,
		"--repo=" + o.Repo,
	}
	cmd := exec.Command(exe, args...)
	cmd.Dir = o.Repo
	cmd.Stdin = nil
	logPath := filepath.Join(homeDir(), ".pakka", "orchestrator.log")
	if f, err := openAppend(logPath); err == nil {
		cmd.Stdout = f
		cmd.Stderr = f
	}
	applyDetach(cmd)
	return cmd
}

// processOne handles one allowlist entry: eligibility check, lock, SHA,
// staleness check, semantic compress, atomic write, meter, state record.
func (o *Orchestrator) processOne(ctx context.Context, rel, level string, state *State, now func() time.Time) {
	abs, ok := o.resolveAndCheckEligible(rel)
	if !ok {
		return
	}

	// Per-file lock under <repo>/.pakka/<encoded>.compress.lock.
	lock, err := acquireFileLock(o.Repo, abs)
	if err != nil {
		o.logf("skip locked: %s (%v)", abs, err)
		return
	}
	defer lock.Release()

	// Resolve the source-of-truth file. First-time compress: copy the live
	// file to <abs>.original.md and use its SHA. Subsequent runs read SHA
	// from the existing .original.md so we measure the *original* drift,
	// not the post-compress drift.
	originalPath := abs + ".original.md"
	if strings.HasSuffix(abs, ".md") {
		originalPath = strings.TrimSuffix(abs, ".md") + ".original.md"
	}
	var origBytes []byte
	if _, err := os.Stat(originalPath); errors.Is(err, os.ErrNotExist) {
		// First run: snapshot the current file as the original. Reuse the
		// in-memory bytes for downstream processing so a concurrent autosave
		// between the backup write and a re-read cannot mismatch the SHA we
		// record against the bytes we compress.
		live, err := os.ReadFile(abs)
		if err != nil {
			o.logf("skip read: %s (%v)", abs, err)
			return
		}
		if err := atomicWrite(originalPath, live); err != nil {
			o.logf("skip backup: %s (%v)", originalPath, err)
			return
		}
		origBytes = live
	} else {
		origBytes, err = os.ReadFile(originalPath)
		if err != nil {
			o.logf("skip read original: %s (%v)", originalPath, err)
			return
		}
	}
	sourceSHA := sha256Hex(origBytes)

	if !state.Stale(abs, level, sourceSHA) {
		o.logf("up to date: %s level=%s", abs, level)
		return
	}

	// Semantic compress.
	if o.Rewriter == nil {
		o.logf("skip no-rewriter: %s (no claude CLI on PATH and no ANTHROPIC_API_KEY)", abs)
		// Don't record success — leave file as-is, leave state untouched.
		return
	}
	out, err := semantic.RunSemantic(ctx, o.Rewriter, string(origBytes), semantic.ParseLevel(level))
	if err != nil {
		var failed *semantic.FailedError
		if errors.As(err, &failed) {
			o.logFailure(abs, level, failed.Violations())
			state.Record(abs, level, sourceSHA, now().UTC().Format(time.RFC3339), false)
			return
		}
		o.logf("rewrite error: %s level=%s err=%v", abs, level, err)
		state.Record(abs, level, sourceSHA, now().UTC().Format(time.RFC3339), false)
		return
	}

	// Validator passed (RunSemantic returned no error). Atomic write of the
	// compressed body to the live file.
	if err := atomicWrite(abs, []byte(out)); err != nil {
		o.logf("write failed: %s (%v)", abs, err)
		return
	}

	// Meter savings (real bytes saved).
	saved := int64(len(origBytes) - len(out))
	if saved > 0 {
		_ = meter.WriteSavings(o.SessionID, o.Repo, saved)
	}

	state.Record(abs, level, sourceSHA, now().UTC().Format(time.RFC3339), true)
	o.logf("compressed: %s level=%s bytes=%d→%d saved=%d",
		abs, level, len(origBytes), len(out), saved)
}

// resolveAndCheckEligible returns (abs, true) when rel exists under o.Repo,
// is a regular file (no symlink), is not above the repo (no `../` escape),
// and has a `.md` extension or is a known memory file.
func (o *Orchestrator) resolveAndCheckEligible(rel string) (string, bool) {
	if strings.Contains(rel, "..") {
		o.logf("skip escape: %s", rel)
		return "", false
	}
	abs := filepath.Join(o.Repo, rel)
	clean, err := filepath.Abs(abs)
	if err != nil {
		o.logf("skip abs-fail: %s (%v)", rel, err)
		return "", false
	}
	repoAbs, err := filepath.Abs(o.Repo)
	if err != nil {
		o.logf("skip repo-abs: %s (%v)", o.Repo, err)
		return "", false
	}
	if !strings.HasPrefix(clean, repoAbs+string(filepath.Separator)) && clean != repoAbs {
		o.logf("skip outside repo: %s", clean)
		return "", false
	}
	info, err := os.Lstat(clean)
	if err != nil {
		// Missing files are silently skipped — allowlist is permissive.
		return "", false
	}
	if info.Mode()&os.ModeSymlink != 0 {
		o.logf("skip symlink: %s", clean)
		return "", false
	}
	if !info.Mode().IsRegular() {
		o.logf("skip non-regular: %s", clean)
		return "", false
	}
	if !isAllowedExt(rel) {
		o.logf("skip ext: %s", clean)
		return "", false
	}
	return clean, true
}

// isAllowedExt returns true for `.md` files and the two memory files we
// always allow even if they live under no extension change.
func isAllowedExt(rel string) bool {
	base := filepath.Base(rel)
	if strings.HasSuffix(strings.ToLower(rel), ".md") {
		return true
	}
	switch base {
	case "LOG.md", "DECISIONS.md":
		return true
	}
	return false
}

// logf writes a single line to LogWriter (or the default orchestrator log).
// Never includes file content — only paths, levels, sizes, error reasons.
func (o *Orchestrator) logf(format string, args ...interface{}) {
	w := o.LogWriter
	if w == nil {
		w = o.ensureDefaultLog()
	}
	if w == nil {
		return
	}
	ts := time.Now().UTC().Format(time.RFC3339)
	fmt.Fprintf(w, "%s orchestrator: %s\n", ts, fmt.Sprintf(format, args...))
}

// ensureDefaultLog opens the production orchestrator log on first call and
// caches the *os.File on the receiver so subsequent logf calls reuse it.
// Opening once per Orchestrator (rather than once per logf) prevents the FD
// leak observed in long-running orchestrator-bg processes.
func (o *Orchestrator) ensureDefaultLog() io.Writer {
	o.logOnce.Do(func() {
		f, err := openAppend(filepath.Join(homeDir(), ".pakka", "orchestrator.log"))
		if err == nil {
			o.defaultLog = f
		}
	})
	if o.defaultLog == nil {
		return nil
	}
	return o.defaultLog
}

// closeDefaultLog closes the cached log file if one was opened. Called by Run
// on completion. Safe to call multiple times.
func (o *Orchestrator) closeDefaultLog() {
	if o.defaultLog != nil {
		_ = o.defaultLog.Close()
		o.defaultLog = nil
	}
}

// logFailure emits a structured line to ~/.pakka/compress-errors.jsonl plus a
// human line to the orchestrator log. Never includes file content.
func (o *Orchestrator) logFailure(absPath, level string, violations []semantic.Violation) {
	o.logf("validator-failed: %s level=%s violations=%d", absPath, level, len(violations))
	kinds := make([]string, 0, len(violations))
	for _, v := range violations {
		kinds = append(kinds, v.Kind)
	}
	entry := map[string]interface{}{
		"ts":         time.Now().UTC().Format(time.RFC3339),
		"file":       absPath,
		"level":      level,
		"violations": kinds,
		"sid":        o.SessionID,
	}
	path := filepath.Join(homeDir(), ".pakka", "compress-errors.jsonl")
	f, err := openAppend(path)
	if err != nil {
		return
	}
	defer f.Close()
	enc := newJSONEncoder(f)
	_ = enc.Encode(entry)
}

// sha256Hex returns the hex-encoded SHA-256 of data.
func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// atomicWrite writes data to <path>.tmp, fsyncs, and renames over path.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return os.TempDir()
}

func openAppend(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
}

