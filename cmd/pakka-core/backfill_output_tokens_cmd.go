package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// BackfillOutputTokensCmd implements "pakka-core backfill-output-tokens".
//
// One-time recovery tool (Pass B/3, v0.9.0): walks ~/.pakka/meter/*.jsonl
// and ~/.claude/projects/*/<session-id>.jsonl, computes per-session output
// tokens, appends a synthetic meter entry per file with output_tokens set.
//
// Idempotency: if a meter file already has any line with output_tokens > 0,
// the file is skipped. Choice rationale: simpler than max(existing, recomputed)
// and avoids touching files that have been backfilled or naturally populated.
type BackfillOutputTokensCmd struct{}

func (c *BackfillOutputTokensCmd) Name() string { return "backfill-output-tokens" }
func (c *BackfillOutputTokensCmd) Run(args []string) error {
	dryRun := false
	for _, a := range args {
		if a == "--dry-run" {
			dryRun = true
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "pakka: backfill-output-tokens: %v\n", err)
		os.Exit(1)
	}
	meterDir := filepath.Join(home, ".pakka", "meter")
	projectsDir := filepath.Join(home, ".claude", "projects")

	stats, err := backfillOutputTokens(meterDir, projectsDir, dryRun)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pakka: backfill-output-tokens: %v\n", err)
		os.Exit(1)
	}

	mode := "applied"
	if dryRun {
		mode = "dry-run"
	}
	fmt.Printf("pakka: backfill-output-tokens (%s): files processed %d, files skipped %d, orphan sessions added %d, total output tokens written %d\n",
		mode, stats.FilesProcessed, stats.FilesSkipped, stats.OrphansAdded, stats.TotalOutputTokens)
	return nil
}

// BackfillStats summarises a backfill run.
type BackfillStats struct {
	FilesProcessed    int
	FilesSkipped      int
	OrphansAdded      int // transcripts with no matching meter file
	TotalOutputTokens int64
}

// backfillOutputTokens walks meterDir for *.jsonl files. For each file:
//   - Reads existing lines. If any line has output_tokens > 0, skip the file
//     (idempotency: file already backfilled or natively populated).
//   - Collects distinct session IDs from the file's lines.
//   - For each session ID, searches projectsDir/<subdir>/<sid>.jsonl and sums
//     output_tokens across transcripts whose filename matches.
//   - When dryRun is false, appends one new synthetic meter line with the
//     computed output_tokens to the file (including 0 when no transcript
//     was found — keeps the dataset complete and signals "we tried").
//
// Returns aggregate stats and an error only on fatal I/O failure
// (unreadable meterDir). Per-file errors are silently skipped.
func backfillOutputTokens(meterDir, projectsDir string, dryRun bool) (BackfillStats, error) {
	var stats BackfillStats

	entries, err := os.ReadDir(meterDir)
	if err != nil {
		return stats, fmt.Errorf("read meter dir: %w", err)
	}

	// Build index of session_id -> transcript paths under projectsDir.
	transcriptsBySID, err := indexTranscripts(projectsDir)
	if err != nil {
		// Treat missing/unreadable projectsDir as "no transcripts" — backfill
		// still writes 0 entries (useful when running on a machine that has
		// rotated all transcripts).
		transcriptsBySID = map[string][]string{}
	}

	processedSIDs := map[string]struct{}{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		path := filepath.Join(meterDir, e.Name())

		alreadyBackfilled, sessIDs, err := scanMeterFile(path)
		if err != nil {
			continue
		}
		if alreadyBackfilled {
			stats.FilesSkipped++
			// Still mark these session IDs as processed so the orphan
			// pass doesn't create a duplicate orphan entry.
			for sid := range sessIDs {
				processedSIDs[sid] = struct{}{}
			}
			continue
		}

		// Sum output tokens across all transcripts for all session IDs.
		var total int64
		for sid := range sessIDs {
			for _, tpath := range transcriptsBySID[sid] {
				total += sumTranscriptOutputTokens(tpath)
			}
		}

		if !dryRun {
			entry := map[string]interface{}{
				"ts":            time.Now().UTC().Format(time.RFC3339Nano),
				"session_id":    pickSID(sessIDs),
				"output_tokens": total,
				"source":        "backfill",
			}
			if err := appendJSONLine(path, entry); err != nil {
				continue
			}
		}

		stats.FilesProcessed++
		stats.TotalOutputTokens += total

		// Track which transcript session IDs we've now attributed to a
		// meter file, so the orphan pass below skips them.
		for sid := range sessIDs {
			processedSIDs[sid] = struct{}{}
		}
	}

	// Orphan pass: transcripts that have NO matching meter file represent
	// sessions where pakka never observed a tool use (e.g., session
	// produced output but no PostToolUse hooks fired, or hooks ran before
	// pakka was installed in that session). The v0.8.x report computed
	// output tokens by walking transcripts unconditionally; to preserve
	// that coverage we create one synthetic meter file per orphan session
	// whose transcript yields > 0 output tokens. New meter file is named
	// orphan-<short-sid>.jsonl to avoid colliding with future native
	// meter writes (those use shortSID of the session_id alone).
	for sid, paths := range transcriptsBySID {
		if _, seen := processedSIDs[sid]; seen {
			continue
		}
		var total int64
		for _, p := range paths {
			total += sumTranscriptOutputTokens(p)
		}
		if total == 0 {
			continue
		}
		stats.OrphansAdded++
		stats.TotalOutputTokens += total
		if dryRun {
			continue
		}
		orphanName := "orphan-" + shortHash(sid) + ".jsonl"
		orphanPath := filepath.Join(meterDir, orphanName)
		// Skip if a previous backfill already created this orphan file.
		if _, err := os.Stat(orphanPath); err == nil {
			continue
		}
		entry := map[string]interface{}{
			"ts":            time.Now().UTC().Format(time.RFC3339Nano),
			"session_id":    sid,
			"output_tokens": total,
			"source":        "backfill-orphan",
		}
		_ = appendJSONLine(orphanPath, entry)
	}

	return stats, nil
}

// shortHash returns the first 8 chars of sid (after stripping non-alnum/dash),
// matching meter's shortSID convention so orphan filenames stay readable.
func shortHash(sid string) string {
	var b strings.Builder
	for _, r := range sid {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') ||
			(r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	s := b.String()
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

// scanMeterFile returns (alreadyBackfilled, sessionIDs, error).
// alreadyBackfilled is true when either: (a) any line has output_tokens > 0
// (native session-end write or prior backfill that found tokens), or
// (b) any line carries the sentinel "source":"backfill" (prior backfill,
// even if 0 tokens). sessionIDs is the set of distinct session_id values.
func scanMeterFile(path string) (bool, map[string]struct{}, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, nil, err
	}
	defer f.Close()

	sids := map[string]struct{}{}
	sc := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 1024*1024)
	already := false
	for sc.Scan() {
		var line struct {
			SessionID    string `json:"session_id"`
			OutputTokens int64  `json:"output_tokens"`
			Source       string `json:"source"`
		}
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
			continue
		}
		if line.OutputTokens > 0 || line.Source == "backfill" {
			already = true
		}
		if line.SessionID != "" {
			sids[line.SessionID] = struct{}{}
		}
	}
	return already, sids, nil
}

// indexTranscripts walks projectsDir/*/*.jsonl and returns a map from
// session_id (the filename sans .jsonl) to a list of transcript paths.
// One session_id may have multiple paths if Claude Code reused the UUID
// across project directories.
func indexTranscripts(projectsDir string) (map[string][]string, error) {
	index := map[string][]string{}
	subs, err := os.ReadDir(projectsDir)
	if err != nil {
		return index, err
	}
	for _, sub := range subs {
		if !sub.IsDir() {
			continue
		}
		subPath := filepath.Join(projectsDir, sub.Name())
		files, err := os.ReadDir(subPath)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			sid := strings.TrimSuffix(f.Name(), ".jsonl")
			full := filepath.Join(subPath, f.Name())
			index[sid] = append(index[sid], full)
		}
	}
	return index, nil
}

// sumTranscriptOutputTokens sums output_tokens across all assistant message
// usage records in a transcript file. Mirrors statusline.sumTranscriptFile's
// dual-shape handling (Shape A: {"message":{"usage":...}}, Shape B: top-level
// {"usage":...}).
func sumTranscriptOutputTokens(path string) int64 {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 4*1024*1024)

	var total int64
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		// Shape A: {"message":{"usage":{...}}}
		var a struct {
			Message struct {
				Usage struct {
					OutputTokens int64 `json:"output_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &a) == nil && a.Message.Usage.OutputTokens != 0 {
			total += a.Message.Usage.OutputTokens
			continue
		}
		// Shape B: {"usage":{...}}
		var b struct {
			Usage struct {
				OutputTokens int64 `json:"output_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal(line, &b) == nil {
			total += b.Usage.OutputTokens
		}
	}
	return total
}

// appendJSONLine appends one JSON object to path followed by a newline.
func appendJSONLine(path string, v interface{}) error {
	encoded, err := json.Marshal(v)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(encoded); err != nil {
		return err
	}
	_, err = f.Write([]byte("\n"))
	return err
}

// pickSID returns one session id from the set (any deterministic choice).
// Used to label the synthetic backfill entry. Empty string if set is empty.
func pickSID(sids map[string]struct{}) string {
	for sid := range sids {
		return sid
	}
	return ""
}
