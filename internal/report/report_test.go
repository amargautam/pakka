package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGatherMeter(t *testing.T) {
	tmp := t.TempDir()
	meterDir := filepath.Join(tmp, "meter")
	auditDir := filepath.Join(tmp, "audit")
	if err := os.MkdirAll(meterDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(auditDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Two session files.
	sess1 := `{"ts":"2026-04-20T10:00:00Z","session_id":"sess-aaa","tokens_used":100,"bytes_saved":0,"tokens_saved_est":0}
{"ts":"2026-04-20T10:05:00Z","session_id":"sess-aaa","tokens_used":200,"bytes_saved":500,"tokens_saved_est":142}
`
	sess2 := `{"ts":"2026-04-22T14:00:00Z","session_id":"sess-bbb","tokens_used":50,"bytes_saved":100,"tokens_saved_est":28}
`
	if err := os.WriteFile(filepath.Join(meterDir, "sess-aaa.jsonl"), []byte(sess1), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(meterDir, "sess-bbb.jsonl"), []byte(sess2), 0644); err != nil {
		t.Fatal(err)
	}

	stats, err := Gather(meterDir, auditDir, "")
	if err != nil {
		t.Fatal(err)
	}

	if stats.SessionCount != 2 {
		t.Errorf("SessionCount = %d, want 2", stats.SessionCount)
	}
	if stats.TotalTokensUsed != 350 {
		t.Errorf("TotalTokensUsed = %d, want 350", stats.TotalTokensUsed)
	}
	if stats.TotalBytesSaved != 600 {
		t.Errorf("TotalBytesSaved = %d, want 600", stats.TotalBytesSaved)
	}
	if stats.TokensSavedEst != 170 {
		t.Errorf("TokensSavedEst = %d, want 170", stats.TokensSavedEst)
	}
	if stats.FirstSession.Format("2006-01-02") != "2026-04-20" {
		t.Errorf("FirstSession = %v, want 2026-04-20", stats.FirstSession)
	}
	if stats.LastSession.Format("2006-01-02") != "2026-04-22" {
		t.Errorf("LastSession = %v, want 2026-04-22", stats.LastSession)
	}
}


func TestGatherMeterOutputTokens(t *testing.T) {
	tmp := t.TempDir()
	meterDir := filepath.Join(tmp, "meter")
	auditDir := filepath.Join(tmp, "audit")
	if err := os.MkdirAll(meterDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(auditDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Two session-end entries for the SAME repo. Each output_tokens value is a
	// repo-wide CUMULATIVE snapshot (not a per-session delta), so the report
	// must take the latest/MAX snapshot (2000), not the sum — summing
	// snapshots triangular-overcounts. Both tagged with the canonical
	// (symlink-resolved) form of tmp, exactly as meter.RepoKey tags entries,
	// so they match the canonicalized repoRoot filter.
	canonTmp := canonicalize(t, tmp)
	sessA := `{"ts":"2026-05-01T10:00:00Z","session_id":"sess-a","repo":"` + canonTmp + `","output_tokens":1000}
`
	sessB := `{"ts":"2026-05-02T10:00:00Z","session_id":"sess-b","repo":"` + canonTmp + `","output_tokens":2000}
`
	if err := os.WriteFile(filepath.Join(meterDir, "sess-a.jsonl"), []byte(sessA), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(meterDir, "sess-b.jsonl"), []byte(sessB), 0644); err != nil {
		t.Fatal(err)
	}

	// Use a tmpdir as fake $HOME so any transcript lookup finds nothing.
	t.Setenv("HOME", tmp)

	stats, err := Gather(meterDir, auditDir, tmp)
	if err != nil {
		t.Fatal(err)
	}

	if stats.OutputTokensTotal != 2000 {
		t.Errorf("OutputTokensTotal = %d, want 2000 (max repo-wide cumulative snapshot, not sum)", stats.OutputTokensTotal)
	}
}

// TestGatherMeterOutputTokensRepoFiltered verifies the output-tokens figure is
// scoped to the requested repo: a larger snapshot tagged to a DIFFERENT repo
// must not leak into this repo's cumulative.
func TestGatherMeterOutputTokensRepoFiltered(t *testing.T) {
	tmp := t.TempDir()
	meterDir := filepath.Join(tmp, "meter")
	auditDir := filepath.Join(tmp, "audit")
	if err := os.MkdirAll(meterDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(auditDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmp)

	mine := `{"ts":"2026-05-01T10:00:00Z","session_id":"s1","repo":"` + canonicalize(t, tmp) + `","output_tokens":500}
`
	other := `{"ts":"2026-05-02T10:00:00Z","session_id":"s2","repo":"/some/other/repo","output_tokens":9999}
`
	if err := os.WriteFile(filepath.Join(meterDir, "s1.jsonl"), []byte(mine), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(meterDir, "s2.jsonl"), []byte(other), 0644); err != nil {
		t.Fatal(err)
	}

	stats, err := Gather(meterDir, auditDir, tmp)
	if err != nil {
		t.Fatal(err)
	}
	if stats.OutputTokensTotal != 500 {
		t.Errorf("OutputTokensTotal = %d, want 500 (other repo's 9999 must not leak)", stats.OutputTokensTotal)
	}
}

// canonicalize returns the absolute symlink-resolved form of path — the same
// canonical form meter.RepoKey produces for entry tags (macOS t.TempDir
// returns /var/... which is a symlink to /private/var/...).
func canonicalize(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	abs, err := filepath.Abs(resolved)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// TestGatherMeterOutputTokensWorkspaceContainsChildRepos verifies the repo
// filter semantics over consistently tagged entries (#10):
//   - a --repo-root pointing at a multi-repo workspace dir matches entries
//     tagged with the workspace root itself AND entries tagged with child
//     repos nested under it;
//   - the figure is the MAX snapshot across all matching entries, never the
//     sum;
//   - filtering by a single child repo excludes its siblings.
func TestGatherMeterOutputTokensWorkspaceContainsChildRepos(t *testing.T) {
	tmp := t.TempDir()
	meterDir := filepath.Join(tmp, "meter")
	auditDir := filepath.Join(tmp, "audit")
	if err := os.MkdirAll(meterDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(auditDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmp)

	// Workspace dir (a real, non-git dir) with two child repo tags plus one
	// snapshot tagged at the workspace root itself.
	ws := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(ws, 0755); err != nil {
		t.Fatal(err)
	}
	canonWS := canonicalize(t, ws)
	repoA := canonWS + "/repo-a"
	repoB := canonWS + "/repo-b"

	lines := `{"ts":"2026-06-01T10:00:00Z","session_id":"s1","repo":"` + repoA + `","output_tokens":100}
{"ts":"2026-06-02T10:00:00Z","session_id":"s2","repo":"` + repoA + `","output_tokens":300}
{"ts":"2026-06-03T10:00:00Z","session_id":"s3","repo":"` + repoB + `","output_tokens":200}
{"ts":"2026-06-04T10:00:00Z","session_id":"s4","repo":"` + canonWS + `","output_tokens":250}
{"ts":"2026-06-05T10:00:00Z","session_id":"s5","repo":"/elsewhere/repo-c","output_tokens":9999}
`
	if err := os.WriteFile(filepath.Join(meterDir, "multi.jsonl"), []byte(lines), 0644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name     string
		repoRoot string
		want     int64
	}{
		// Workspace filter contains the workspace-root tag and both child
		// repos: max(100, 300, 200, 250) = 300 — NOT the sum (850), and the
		// foreign repo's 9999 must not leak.
		{"workspace root", ws, 300},
		{"child repo a", repoA, 300},
		{"child repo b", repoB, 200},
	}
	for _, tc := range cases {
		stats, err := Gather(meterDir, auditDir, tc.repoRoot)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if stats.OutputTokensTotal != tc.want {
			t.Errorf("%s: OutputTokensTotal = %d, want %d (max snapshot among contained repos, not sum)",
				tc.name, stats.OutputTokensTotal, tc.want)
		}
	}
}

func TestGatherAudit(t *testing.T) {
	tmp := t.TempDir()
	meterDir := filepath.Join(tmp, "meter")
	auditDir := filepath.Join(tmp, "audit")
	if err := os.MkdirAll(meterDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(auditDir, 0755); err != nil {
		t.Fatal(err)
	}

	auditData := `{"schema":"pakka.audit.v1"}
{"ts":"2026-04-20T10:00:00Z","session_id":"sess-aaa","kind":"tool_use","tool":"Read","result":"ok"}
{"ts":"2026-04-20T10:01:00Z","session_id":"sess-aaa","kind":"tool_use","tool":"Read","result":"ok"}
{"ts":"2026-04-20T10:02:00Z","session_id":"sess-aaa","kind":"tool_use","tool":"Bash","result":"ok"}
{"ts":"2026-04-20T10:03:00Z","session_id":"sess-aaa","kind":"tool_use","tool":"Edit","result":"ok"}
`
	if err := os.WriteFile(filepath.Join(auditDir, "sess-aaa.jsonl"), []byte(auditData), 0644); err != nil {
		t.Fatal(err)
	}

	stats, err := Gather(meterDir, auditDir, "")
	if err != nil {
		t.Fatal(err)
	}

	if stats.AuditEventCount != 4 {
		t.Errorf("AuditEventCount = %d, want 4", stats.AuditEventCount)
	}
	if stats.ToolUseCounts["Read"] != 2 {
		t.Errorf("ToolUseCounts[Read] = %d, want 2", stats.ToolUseCounts["Read"])
	}
	if stats.ToolUseCounts["Bash"] != 1 {
		t.Errorf("ToolUseCounts[Bash] = %d, want 1", stats.ToolUseCounts["Bash"])
	}
	if stats.ToolUseCounts["Edit"] != 1 {
		t.Errorf("ToolUseCounts[Edit] = %d, want 1", stats.ToolUseCounts["Edit"])
	}
}

func TestGatherBothEmpty(t *testing.T) {
	_, err := Gather("/nonexistent/meter", "/nonexistent/audit", "")
	if err == nil {
		t.Error("expected error when both dirs are unreadable, got nil")
	}
}

func TestGatherOneDirMissing(t *testing.T) {
	tmp := t.TempDir()
	meterDir := filepath.Join(tmp, "meter")
	if err := os.MkdirAll(meterDir, 0755); err != nil {
		t.Fatal(err)
	}

	sess := `{"ts":"2026-04-20T10:00:00Z","session_id":"sess-aaa","tokens_used":100,"bytes_saved":0,"tokens_saved_est":0}
`
	if err := os.WriteFile(filepath.Join(meterDir, "sess-aaa.jsonl"), []byte(sess), 0644); err != nil {
		t.Fatal(err)
	}

	stats, err := Gather(meterDir, "/nonexistent/audit", "")
	if err != nil {
		t.Fatalf("expected nil error when one dir exists, got: %v", err)
	}
	if stats.SessionCount != 1 {
		t.Errorf("SessionCount = %d, want 1", stats.SessionCount)
	}
}

func TestFormatMarkdown(t *testing.T) {
	stats := &Stats{
		SessionCount:    12,
		TotalTokensUsed: 45200,
		TotalBytesSaved: 3400,
		TokensSavedEst:  971,
		AuditEventCount: 468,
		ToolUseCounts: map[string]int{
			"Bash": 234,
			"Read": 189,
			"Edit": 45,
		},
		GateVerdicts:  8,
		GatePassCount: 7,
	}

	// Set times via parsing.
	stats.FirstSession, _ = parseTime("2026-04-20T10:00:00Z")
	stats.LastSession, _ = parseTime("2026-04-24T18:00:00Z")

	output := FormatMarkdown(stats, "0.1.0-dev")

	// Check required sections.
	checks := []string{
		"# RECEIPTS.md",
		"version: v0.1.0-dev",
		"## build stats",
		"| sessions | 12 |",
		"| first session | 2026-04-20 |",
		"| last session | 2026-04-24 |",
		"| total tokens used | 45,200 |",
		"| bytes saved (V2+V3+V4 compression) | 3,400 |",
		"| est. tokens saved (bytes ÷ 3.5) | 971 |",
		"## tool usage",
		"| Bash | 234 |",
		"| Read | 189 |",
		"| Edit | 45 |",
		"## output compression savings (V1 — calibrated bench)",
		"| super-ultra | **~66%** | **~$1.65/MTok output** |",
		"At Sonnet 4.6 pricing ($15/MTok output)",
		"Total estimated savings",
		"## review gate",
		"| verdicts run | 8 |",
		"| verdicts passed | 7 |",
		"| pass rate | 87.5% |",
		"Generated by `pakka-core report`. Apache-2.0.",
	}

	for _, want := range checks {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q", want)
		}
	}
}

func TestFormatMarkdownNoVerdicts(t *testing.T) {
	stats := &Stats{
		ToolUseCounts: make(map[string]int),
	}

	output := FormatMarkdown(stats, "0.1.0-dev")

	if !strings.Contains(output, "| pass rate | — |") {
		t.Error("expected dash for pass rate when no verdicts")
	}
}

func TestFormatMarkdownToolsSortedByCount(t *testing.T) {
	stats := &Stats{
		ToolUseCounts: map[string]int{
			"Edit": 10,
			"Read": 50,
			"Bash": 30,
		},
	}

	output := FormatMarkdown(stats, "0.1.0-dev")

	readIdx := strings.Index(output, "| Read |")
	bashIdx := strings.Index(output, "| Bash |")
	editIdx := strings.Index(output, "| Edit |")

	if readIdx > bashIdx || bashIdx > editIdx {
		t.Errorf("tools not sorted by count descending: Read@%d, Bash@%d, Edit@%d", readIdx, bashIdx, editIdx)
	}
}

func TestFmtInt(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0"},
		{42, "42"},
		{999, "999"},
		{1000, "1,000"},
		{45200, "45,200"},
		{1234567, "1,234,567"},
		{-500, "-500"},
	}
	for _, tt := range tests {
		got := fmtInt(tt.in)
		if got != tt.want {
			t.Errorf("fmtInt(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func parseTime(s string) (time.Time, error) {
	return time.Parse(time.RFC3339, s)
}
