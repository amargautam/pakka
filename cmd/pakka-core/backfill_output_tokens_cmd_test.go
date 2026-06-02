package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBackfillOutputTokens drives backfillOutputTokens against a synthetic
// meter dir and synthetic transcripts dir. After the call, every meter file
// should contain at least one line with output_tokens matching the sum of
// output_tokens across that session's transcripts.
func TestBackfillOutputTokens(t *testing.T) {
	tmp := t.TempDir()
	meterDir := filepath.Join(tmp, "meter")
	projectsDir := filepath.Join(tmp, "projects")
	if err := os.MkdirAll(meterDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Session A: meter file with session_id "aaa-1111-2222-3333-4444";
	// transcript with two assistant lines summing to 700 output tokens.
	sessA := "aaa-1111-2222-3333-4444"
	meterA := `{"ts":"2026-05-01T10:00:00Z","session_id":"` + sessA + `","repo":"/r","tokens_used":100}` + "\n"
	if err := os.WriteFile(filepath.Join(meterDir, "aaa-1111.jsonl"), []byte(meterA), 0644); err != nil {
		t.Fatal(err)
	}
	projA := filepath.Join(projectsDir, "proj-a")
	if err := os.MkdirAll(projA, 0755); err != nil {
		t.Fatal(err)
	}
	transA := `{"message":{"usage":{"output_tokens":300}}}` + "\n" +
		`{"message":{"usage":{"output_tokens":400}}}` + "\n"
	if err := os.WriteFile(filepath.Join(projA, sessA+".jsonl"), []byte(transA), 0644); err != nil {
		t.Fatal(err)
	}

	// Session B: meter has session_id "bbb-9999-8888-7777-6666";
	// transcript with one line of 1500 output tokens.
	sessB := "bbb-9999-8888-7777-6666"
	meterB := `{"ts":"2026-05-02T10:00:00Z","session_id":"` + sessB + `","repo":"/r","bytes_saved":50}` + "\n"
	if err := os.WriteFile(filepath.Join(meterDir, "bbb-9999.jsonl"), []byte(meterB), 0644); err != nil {
		t.Fatal(err)
	}
	projB := filepath.Join(projectsDir, "proj-b")
	if err := os.MkdirAll(projB, 0755); err != nil {
		t.Fatal(err)
	}
	transB := `{"message":{"usage":{"output_tokens":1500}}}` + "\n"
	if err := os.WriteFile(filepath.Join(projB, sessB+".jsonl"), []byte(transB), 0644); err != nil {
		t.Fatal(err)
	}

	// Session C: meter exists, no matching transcript → output_tokens=0
	// should still be written (idempotent baseline).
	sessC := "ccc-no-transcript-here-zzzz"
	meterC := `{"ts":"2026-05-03T10:00:00Z","session_id":"` + sessC + `","repo":"/r","tokens_used":10}` + "\n"
	if err := os.WriteFile(filepath.Join(meterDir, "ccc-nooo.jsonl"), []byte(meterC), 0644); err != nil {
		t.Fatal(err)
	}

	stats, err := backfillOutputTokens(meterDir, projectsDir, false)
	if err != nil {
		t.Fatal(err)
	}

	checks := map[string]int64{
		filepath.Join(meterDir, "aaa-1111.jsonl"): 700,
		filepath.Join(meterDir, "bbb-9999.jsonl"): 1500,
		filepath.Join(meterDir, "ccc-nooo.jsonl"): 0,
	}
	for path, want := range checks {
		got := maxOutputTokensInFile(t, path)
		if got != want {
			t.Errorf("%s: max output_tokens = %d, want %d", filepath.Base(path), got, want)
		}
	}

	if stats.FilesProcessed != 3 {
		t.Errorf("FilesProcessed = %d, want 3", stats.FilesProcessed)
	}
	if stats.TotalOutputTokens != 2200 {
		t.Errorf("TotalOutputTokens = %d, want 2200", stats.TotalOutputTokens)
	}

	// Idempotency: second invocation must not change totals or duplicate writes.
	beforeBytes, _ := os.ReadFile(filepath.Join(meterDir, "aaa-1111.jsonl"))
	stats2, err := backfillOutputTokens(meterDir, projectsDir, false)
	if err != nil {
		t.Fatal(err)
	}
	afterBytes, _ := os.ReadFile(filepath.Join(meterDir, "aaa-1111.jsonl"))
	if string(beforeBytes) != string(afterBytes) {
		t.Errorf("second run mutated aaa-1111.jsonl; want idempotent\nbefore=%q\nafter=%q", beforeBytes, afterBytes)
	}
	if stats2.FilesProcessed != 0 {
		t.Errorf("second run FilesProcessed = %d, want 0 (all skipped)", stats2.FilesProcessed)
	}
}

func TestBackfillOutputTokensDryRun(t *testing.T) {
	tmp := t.TempDir()
	meterDir := filepath.Join(tmp, "meter")
	projectsDir := filepath.Join(tmp, "projects")
	if err := os.MkdirAll(meterDir, 0755); err != nil {
		t.Fatal(err)
	}

	sess := "dryrun01-2222-3333-4444-5555"
	meterContent := `{"ts":"2026-05-01T10:00:00Z","session_id":"` + sess + `","repo":"/r","tokens_used":100}` + "\n"
	meterPath := filepath.Join(meterDir, "dryrun01.jsonl")
	if err := os.WriteFile(meterPath, []byte(meterContent), 0644); err != nil {
		t.Fatal(err)
	}
	proj := filepath.Join(projectsDir, "proj")
	if err := os.MkdirAll(proj, 0755); err != nil {
		t.Fatal(err)
	}
	trans := `{"message":{"usage":{"output_tokens":999}}}` + "\n"
	if err := os.WriteFile(filepath.Join(proj, sess+".jsonl"), []byte(trans), 0644); err != nil {
		t.Fatal(err)
	}

	before, _ := os.ReadFile(meterPath)
	if _, err := backfillOutputTokens(meterDir, projectsDir, true); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(meterPath)
	if string(before) != string(after) {
		t.Errorf("dry-run mutated file; before=%q after=%q", before, after)
	}
}

// maxOutputTokensInFile returns the largest output_tokens value across all
// JSONL lines in path.
func maxOutputTokensInFile(t *testing.T, path string) int64 {
	t.Helper()
	bs, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var max int64
	for _, line := range strings.Split(strings.TrimSpace(string(bs)), "\n") {
		if line == "" {
			continue
		}
		var probe struct {
			OutputTokens int64 `json:"output_tokens"`
		}
		if err := json.Unmarshal([]byte(line), &probe); err != nil {
			continue
		}
		if probe.OutputTokens > max {
			max = probe.OutputTokens
		}
	}
	return max
}
