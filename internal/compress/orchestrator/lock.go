package orchestrator

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// FileLock is a simple advisory lock implemented via O_EXCL on a sentinel
// file. flock(2) would be portable across this set of OSes via x/sys, but the
// stdlib O_EXCL contract is sufficient for our single-host orchestrator and
// keeps deps zero.
type FileLock struct {
	path string
	f    *os.File
}

// acquireFileLock takes a lock keyed on absPath. The lock filename is
// <repo>/.pakka/<sha1(absPath)>.compress.lock. Returns an error when the lock
// is already held by another process; the caller treats that as "skip".
func acquireFileLock(repoDir, absPath string) (*FileLock, error) {
	dir := filepath.Join(repoDir, ".pakka")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	hash := sha1.Sum([]byte(absPath))
	name := hex.EncodeToString(hash[:8]) + ".compress.lock"
	lockPath := filepath.Join(dir, name)
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("lock busy: %w", err)
	}
	if _, werr := fmt.Fprintf(f, "pid=%d\n", os.Getpid()); werr != nil {
		_ = f.Close()
		_ = os.Remove(lockPath)
		return nil, werr
	}
	return &FileLock{path: lockPath, f: f}, nil
}

// Release closes and removes the lock file.
//
// Purpose: Idempotent unlock; safe to defer on error paths.
// Errors: Swallowed — release is best-effort.
func (l *FileLock) Release() {
	if l == nil {
		return
	}
	if l.f != nil {
		_ = l.f.Close()
	}
	if l.path != "" {
		_ = os.Remove(l.path)
	}
}

// newJSONEncoder is a tiny shim so orchestrator.go can encode without
// importing encoding/json directly (keeps imports tidy in that file).
func newJSONEncoder(w io.Writer) *json.Encoder { return json.NewEncoder(w) }
