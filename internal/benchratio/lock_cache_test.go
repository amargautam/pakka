package benchratio

import (
	"os"
	"sync"
	"testing"
	"time"
)

// TestUpdateSerializesConcurrentWriters covers finding 1: concurrent locked
// updates must not drop each other's sample. Without the lock the
// load-modify-save race would leave samples < N.
func TestUpdateSerializesConcurrentWriters(t *testing.T) {
	home := t.TempDir()
	const N = 12

	var wg sync.WaitGroup
	var mu sync.Mutex
	var errs []error
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := UpdateFrom(home, func(s *Store) {
				s.Record("r", "m", "super-ultra", 0.5, ts)
			})
			if err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(errs) != 0 {
		t.Fatalf("locked updates errored: %v", errs)
	}

	got, err := LoadFrom(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 1 {
		t.Fatalf("want 1 merged entry, got %d", len(got.Entries))
	}
	if got.Entries[0].Samples != N {
		t.Errorf("lost updates: want samples=%d, got %d", N, got.Entries[0].Samples)
	}
}

// TestUpdateWaitsForHeldLock asserts Update blocks while another holder owns
// the lock and completes once it is released.
func TestUpdateWaitsForHeldLock(t *testing.T) {
	home := t.TempDir()
	lock, err := acquireLock(home)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		done <- UpdateFrom(home, func(s *Store) {
			s.Record("r", "m", "super-ultra", 0.7, ts)
		})
	}()

	// While the lock is held, the update must not complete.
	time.Sleep(25 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("Update completed while lock was held")
	default:
	}

	lock.release()
	if err := <-done; err != nil {
		t.Fatalf("Update after release failed: %v", err)
	}
	got, _ := LoadFrom(home)
	if len(got.Entries) != 1 || got.Entries[0].Samples != 1 {
		t.Fatalf("update after release wrong: %+v", got.Entries)
	}
}

// TestAcquireLockReclaimsStale asserts a lock file older than lockStaleAfter is
// reclaimed rather than wedging future writers.
func TestAcquireLockReclaimsStale(t *testing.T) {
	home := t.TempDir()
	path := lockPath(home)
	if err := os.MkdirAll(home+"/.pakka", 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("pid=99999\n"), 0644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * lockStaleAfter)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	if err := UpdateFrom(home, func(s *Store) {
		s.Record("r", "m", "super-ultra", 0.5, ts)
	}); err != nil {
		t.Fatalf("stale lock should be reclaimed: %v", err)
	}
	got, _ := LoadFrom(home)
	if len(got.Entries) != 1 {
		t.Fatalf("stale-reclaim update did not persist: %+v", got.Entries)
	}
}

// TestLoadCachedParsesOncePerMtime covers finding 2: an unchanged file is
// parsed once; a changed mtime forces a re-parse.
func TestLoadCachedParsesOncePerMtime(t *testing.T) {
	home := t.TempDir()
	ResetCache()
	start := parseCount

	s := &Store{}
	s.Record("r", "m", "super-ultra", 0.5, ts)
	if err := s.SaveTo(home); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadCached(home); err != nil {
		t.Fatal(err)
	}
	if parseCount != start+1 {
		t.Fatalf("first read should parse once: delta %d", parseCount-start)
	}

	// Unchanged file: served from the memo, no re-parse.
	if _, err := LoadCached(home); err != nil {
		t.Fatal(err)
	}
	if parseCount != start+1 {
		t.Fatalf("unchanged file must not re-parse: delta %d", parseCount-start)
	}

	// Bump mtime -> cache invalidated -> re-parse.
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(DefaultPath(home), future, future); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCached(home); err != nil {
		t.Fatal(err)
	}
	if parseCount != start+2 {
		t.Fatalf("changed mtime must re-parse: delta %d", parseCount-start)
	}
}

// TestLoadCachedMissingFileEmpty asserts a missing file yields an empty store
// without polluting the cache or counting a parse.
func TestLoadCachedMissingFileEmpty(t *testing.T) {
	home := t.TempDir()
	ResetCache()
	start := parseCount
	got, err := LoadCached(home)
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if len(got.Entries) != 0 {
		t.Errorf("want empty store, got %d", len(got.Entries))
	}
	if parseCount != start {
		t.Errorf("missing file must not count a parse: delta %d", parseCount-start)
	}
}
