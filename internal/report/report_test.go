package report

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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

	output := FormatMarkdown(stats, "v0.1.0-dev")

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

// totalSavingsFromMarkdown extracts the "~$X.XX" total-savings dollar figure
// from a rendered RECEIPTS.md.
func totalSavingsFromMarkdown(t *testing.T, md string) float64 {
	t.Helper()
	re := regexp.MustCompile(`Total estimated savings: ~\$([0-9]+\.[0-9]+)`)
	m := re.FindStringSubmatch(md)
	if m == nil {
		t.Fatalf("no total-savings line in markdown:\n%s", md)
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		t.Fatalf("parse total savings %q: %v", m[1], err)
	}
	return v
}

// TestFormatMarkdownInputSavingsVariesWithCacheMix asserts the reported $
// savings are behavioral on the cache mix: for an identical TokensSavedEst and
// identical output volume, a cache-heavy session reports strictly LOWER total
// savings than an all-fresh session (input tokens re-billed at the ~0.1×
// cache-read rate), and the all-fresh session reproduces the old flat
// fresh-input-rate figure. Only the input side varies; output is unchanged.
func TestFormatMarkdownInputSavingsVariesWithCacheMix(t *testing.T) {
	base := Stats{
		SessionCount:      5,
		OutputTokensTotal: 400_000,
		TotalBytesSaved:   3400,
		TokensSavedEst:    971_000,
	}

	fresh := base
	fresh.InputTokens = 1_000_000 // all fresh → multiplier 1.0

	cacheHeavy := base
	cacheHeavy.InputTokens = 100_000
	cacheHeavy.CacheReadTokens = 900_000 // 90% cache read → multiplier 0.19

	noTelemetry := base // all cache fields zero → fallback 1.0

	freshTotal := totalSavingsFromMarkdown(t, FormatMarkdown(&fresh, "0.1.0-dev"))
	cacheTotal := totalSavingsFromMarkdown(t, FormatMarkdown(&cacheHeavy, "0.1.0-dev"))
	noTelTotal := totalSavingsFromMarkdown(t, FormatMarkdown(&noTelemetry, "0.1.0-dev"))

	if cacheTotal >= freshTotal {
		t.Fatalf("cache-heavy total $%.2f must be strictly below all-fresh $%.2f", cacheTotal, freshTotal)
	}
	// No-telemetry must match all-fresh (both multiplier 1.0) — unchanged behavior.
	if noTelTotal != freshTotal {
		t.Errorf("no-telemetry total $%.2f must equal all-fresh $%.2f (flat-rate fallback)", noTelTotal, freshTotal)
	}
	// The gap is purely the input side; sanity-check its direction and size.
	// Input side fresh = 0.971M * $3 = ~$2.913; cache-heavy = *0.19 = ~$0.553.
	if gap := freshTotal - cacheTotal; gap < 2.0 {
		t.Errorf("input-side savings gap $%.2f too small — blending not applied", gap)
	}
}

// AC3: RECEIPTS gains a review-gate calibration section. With an artifact
// present the section reports the artifact's rates; absent → "unmeasured".
func TestFormatMarkdown_calibrationSection(t *testing.T) {
	// Present: measured rates render.
	withArtifact := &Stats{
		CalibrationFound:     true,
		CalibrationRecall:    0.8,
		CalibrationPrecision: 0.75,
		CalibrationFPRate:    0.33,
		CalibrationN:         10,
		CalibrationModel:     "claude-sonnet-4-6",
		CalibrationDate:      "2026-07-27",
	}
	md := FormatMarkdown(withArtifact, "0.1.0-dev")
	if !strings.Contains(md, "review gate calibration") {
		t.Fatalf("missing calibration section:\n%s", md)
	}
	for _, want := range []string{"80%", "75%", "claude-sonnet-4-6", "2026-07-27"} {
		if !strings.Contains(md, want) {
			t.Errorf("calibration section missing %q", want)
		}
	}
	if strings.Contains(md, "unmeasured") {
		t.Errorf("measured artifact must not render 'unmeasured'")
	}

	// Absent: the literal "unmeasured" string is asserted per AC3.
	none := &Stats{CalibrationFound: false}
	md2 := FormatMarkdown(none, "0.1.0-dev")
	if !strings.Contains(md2, "review gate calibration") {
		t.Fatalf("missing calibration section (absent case):\n%s", md2)
	}
	if !strings.Contains(md2, "unmeasured") {
		t.Errorf("no-artifact case must contain 'unmeasured'")
	}
}

// Finding 2: a degraded calibration run renders a warning banner and marks the
// rate cells, instead of presenting bare numbers as reviewer performance.
func TestFormatMarkdown_calibrationDegraded(t *testing.T) {
	degraded := &Stats{
		CalibrationFound:     true,
		CalibrationRecall:    0.0,
		CalibrationPrecision: 0.0,
		CalibrationFPRate:    0.0,
		CalibrationN:         10,
		CalibrationModel:     "claude-sonnet-4-6",
		CalibrationDate:      "2026-07-27",
		CalibrationDegraded:  true,
		CalibrationScored:    10,
	}
	md := FormatMarkdown(degraded, "0.1.0-dev")
	if !strings.Contains(md, "DEGRADED RUN") {
		t.Errorf("degraded run must render a DEGRADED banner:\n%s", md)
	}
	if !strings.Contains(md, "⚠️ degraded") {
		t.Errorf("degraded run must mark the rate cells with an inline caveat")
	}

	// A healthy run carries neither the banner nor the inline mark.
	healthy := *degraded
	healthy.CalibrationDegraded = false
	md2 := FormatMarkdown(&healthy, "0.1.0-dev")
	if strings.Contains(md2, "DEGRADED RUN") || strings.Contains(md2, "⚠️ degraded") {
		t.Errorf("healthy run must not render degraded annotations:\n%s", md2)
	}
}

// Version header resolves from the plugin manifest at generation time and
// must VARY with plugin.json content — never a stale literal. Missing or
// malformed manifest → "unknown".
func TestResolveVersion_variesWithManifest(t *testing.T) {
	writeManifest := func(t *testing.T, content string) string {
		t.Helper()
		root := t.TempDir()
		dir := filepath.Join(root, ".claude-plugin")
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		return root
	}

	// Isolate from the real repo: the cwd/git-toplevel fallback must not pick
	// up pakka's own manifest.
	t.Chdir(t.TempDir())

	rootA := writeManifest(t, `{"name":"pakka","version":"0.19.0"}`)
	rootB := writeManifest(t, `{"name":"pakka","version":"1.2.3"}`)

	gotA := ResolveVersion(rootA)
	gotB := ResolveVersion(rootB)
	if gotA != "v0.19.0" {
		t.Errorf("ResolveVersion(rootA) = %q, want v0.19.0", gotA)
	}
	if gotB != "v1.2.3" {
		t.Errorf("ResolveVersion(rootB) = %q, want v1.2.3", gotB)
	}
	if gotA == gotB {
		t.Errorf("version must vary with manifest content; both = %q", gotA)
	}

	// Missing manifest → unknown.
	if got := ResolveVersion(t.TempDir()); got != "unknown" {
		t.Errorf("missing manifest: ResolveVersion = %q, want unknown", got)
	}
	// Malformed JSON → unknown.
	if got := ResolveVersion(writeManifest(t, `{not json`)); got != "unknown" {
		t.Errorf("malformed manifest: ResolveVersion = %q, want unknown", got)
	}
	// Empty version field → unknown.
	if got := ResolveVersion(writeManifest(t, `{"name":"pakka"}`)); got != "unknown" {
		t.Errorf("empty version field: ResolveVersion = %q, want unknown", got)
	}

	// The rendered header carries the resolved string verbatim.
	s := &Stats{}
	if md := FormatMarkdown(s, gotA); !strings.Contains(md, "version: v0.19.0") {
		t.Errorf("header must render resolved manifest version:\n%s", md)
	}
	if md := FormatMarkdown(s, gotB); !strings.Contains(md, "version: v1.2.3") {
		t.Errorf("header must render resolved manifest version:\n%s", md)
	}
	if md := FormatMarkdown(s, "unknown"); !strings.Contains(md, "version: unknown") {
		t.Errorf("header must render unknown when manifest unresolvable:\n%s", md)
	}
}

// Production shape: `make self-report` invokes the report with --repo-root=..
// (the workspace parent, which has NO .claude-plugin/) while the CWD is the
// pakka repo itself. ResolveVersion must fall back to the invoking repo's
// manifest (from CWD) instead of rendering "unknown" on every real run, and
// the result must VARY with that manifest's content.
func TestResolveVersion_fallsBackToCwdManifest(t *testing.T) {
	parent := t.TempDir() // repoRoot passed in — no manifest here
	child := filepath.Join(parent, "repo")
	dir := filepath.Join(child, ".claude-plugin")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(dir, "plugin.json")
	if err := os.WriteFile(manifest, []byte(`{"name":"pakka","version":"7.7.7"}`), 0644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(child)

	if got := ResolveVersion(parent); got != "v7.7.7" {
		t.Errorf("repoRoot without manifest, cwd with manifest: ResolveVersion = %q, want v7.7.7", got)
	}

	// Behavioral: result varies with the cwd manifest's content.
	if err := os.WriteFile(manifest, []byte(`{"name":"pakka","version":"8.8.8"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if got := ResolveVersion(parent); got != "v8.8.8" {
		t.Errorf("after manifest change: ResolveVersion = %q, want v8.8.8", got)
	}

	// repoRoot manifest still wins over the cwd fallback when present.
	rootDir := filepath.Join(parent, ".claude-plugin")
	if err := os.MkdirAll(rootDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "plugin.json"), []byte(`{"version":"9.9.9"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if got := ResolveVersion(parent); got != "v9.9.9" {
		t.Errorf("repoRoot manifest present: ResolveVersion = %q, want v9.9.9 (repoRoot wins)", got)
	}
}

// Hardening: a malformed or hostile manifest must not smuggle newlines,
// markdown, or unbounded content into the RECEIPTS header — invalid version
// strings and oversized manifests resolve to "unknown".
func TestResolveVersion_rejectsHostileManifest(t *testing.T) {
	t.Chdir(t.TempDir())

	write := func(t *testing.T, content string) string {
		t.Helper()
		root := t.TempDir()
		dir := filepath.Join(root, ".claude-plugin")
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		return root
	}

	cases := map[string]string{
		"newline":       `{"version":"0.1.0\nversion: v9.9.9"}`,
		"backticks":     "{\"version\":\"0.1.0`rm -rf`\"}",
		"markdown pipe": `{"version":"0.1.0 | pwned"}`,
		"leading dash":  `{"version":"-0.1.0"}`,
		"too long":      `{"version":"` + strings.Repeat("1", 65) + `"}`,
	}
	for name, content := range cases {
		if got := ResolveVersion(write(t, content)); got != "unknown" {
			t.Errorf("%s: ResolveVersion = %q, want unknown", name, got)
		}
	}

	// Oversized manifest (>64KiB) is rejected without reading it all.
	big := `{"padding":"` + strings.Repeat("x", 70*1024) + `","version":"1.0.0"}`
	if got := ResolveVersion(write(t, big)); got != "unknown" {
		t.Errorf("oversized manifest: ResolveVersion = %q, want unknown", got)
	}

	// Sanity: a well-formed version at the 64-char limit still resolves.
	ok := `{"version":"` + strings.Repeat("1", 64) + `"}`
	if got := ResolveVersion(write(t, ok)); got != "v"+strings.Repeat("1", 64) {
		t.Errorf("64-char version: ResolveVersion = %q, want it accepted", got)
	}
}

// The calibration methodology sentence and model row must agree: with a model
// present the sentence claims rates carry model; without one the row reads
// "not recorded" and the sentence does not claim a model. The two cases must
// render DIFFERENT output.
func TestFormatMarkdown_calibrationModelSentenceVaries(t *testing.T) {
	base := Stats{
		CalibrationFound:     true,
		CalibrationRecall:    0.8,
		CalibrationPrecision: 0.75,
		CalibrationFPRate:    0.33,
		CalibrationN:         10,
		CalibrationDate:      "2026-07-27",
	}

	withModel := base
	withModel.CalibrationModel = "claude-sonnet-4-6"
	mdWith := FormatMarkdown(&withModel, "v0.1.0")

	withoutModel := base // CalibrationModel empty
	mdWithout := FormatMarkdown(&withoutModel, "v0.1.0")

	const sentenceWith = "Rates carry n and model — no averaging across models."
	const sentenceWithout = "Rates carry n; model is recorded when the headless runner reports it — no averaging across models."

	// With model: original sentence + real model row.
	if !strings.Contains(mdWith, sentenceWith) {
		t.Errorf("with-model case missing sentence %q:\n%s", sentenceWith, mdWith)
	}
	if strings.Contains(mdWith, sentenceWithout) {
		t.Errorf("with-model case must not render the no-model sentence")
	}
	if !strings.Contains(mdWith, "| model | claude-sonnet-4-6 |") {
		t.Errorf("with-model case missing model row:\n%s", mdWith)
	}
	if strings.Contains(mdWith, "not recorded") {
		t.Errorf("with-model case must not render 'not recorded'")
	}

	// Without model: honest sentence + "not recorded" row, no overclaim.
	if !strings.Contains(mdWithout, sentenceWithout) {
		t.Errorf("no-model case missing sentence %q:\n%s", sentenceWithout, mdWithout)
	}
	if strings.Contains(mdWithout, sentenceWith) {
		t.Errorf("no-model case must not claim rates carry model")
	}
	if !strings.Contains(mdWithout, "| model | not recorded |") {
		t.Errorf("no-model case missing 'not recorded' model row:\n%s", mdWithout)
	}
	if strings.Contains(mdWithout, "| model | unknown |") {
		t.Errorf("no-model case must not render 'unknown' model row")
	}

	if mdWith == mdWithout {
		t.Errorf("calibration section must vary with model presence")
	}
}

// Finding 1: the run-health counts (scored/timeout/error) surface in RECEIPTS.
func TestFormatMarkdown_calibrationCounts(t *testing.T) {
	s := &Stats{
		CalibrationFound:   true,
		CalibrationN:       8,
		CalibrationScored:  8,
		CalibrationTimeout: 1,
		CalibrationErrors:  2,
		CalibrationDate:    "2026-07-27",
	}
	md := FormatMarkdown(s, "0.1.0-dev")
	if !strings.Contains(md, "seeds scored / timeout / error") || !strings.Contains(md, "8 / 1 / 2") {
		t.Errorf("counts row missing or wrong:\n%s", md)
	}
}

// AC3: Gather reads the newest calibration-*.json under repoRoot and populates
// the Calibration* fields, choosing the lexically-latest (newest date) file.
func TestGather_readsNewestCalibration(t *testing.T) {
	root := t.TempDir()
	meterDir := filepath.Join(root, ".meter")
	auditDir := filepath.Join(root, ".audit")
	os.MkdirAll(meterDir, 0755)
	os.MkdirAll(auditDir, 0755)

	resultsDir := filepath.Join(root, "benchmarks", "results")
	if err := os.MkdirAll(resultsDir, 0755); err != nil {
		t.Fatal(err)
	}
	older := `{"date":"2026-07-01","threshold":80,"aggregate":{"recall":0.5,"precision":0.5,"fpRate":0.1,"n":10,"model":"old-model"}}`
	newer := `{"date":"2026-07-27","threshold":80,"aggregate":{"recall":0.9,"precision":0.8,"fpRate":0.2,"n":10,"model":"new-model","degraded":true,"counts":{"scored":10,"timeout":2,"error":1}}}`
	if err := os.WriteFile(filepath.Join(resultsDir, "calibration-2026-07-01.json"), []byte(older), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resultsDir, "calibration-2026-07-27.json"), []byte(newer), 0644); err != nil {
		t.Fatal(err)
	}

	stats, err := Gather(meterDir, auditDir, root)
	if err != nil {
		t.Fatal(err)
	}
	if !stats.CalibrationFound {
		t.Fatal("CalibrationFound=false, want true")
	}
	if stats.CalibrationModel != "new-model" {
		t.Errorf("model=%q, want new-model (newest artifact)", stats.CalibrationModel)
	}
	if stats.CalibrationRecall != 0.9 {
		t.Errorf("recall=%v, want 0.9", stats.CalibrationRecall)
	}
	if !stats.CalibrationDegraded {
		t.Errorf("CalibrationDegraded=false, want true (artifact carries degraded)")
	}
	if stats.CalibrationScored != 10 || stats.CalibrationTimeout != 2 || stats.CalibrationErrors != 1 {
		t.Errorf("counts scored/timeout/error = %d/%d/%d, want 10/2/1",
			stats.CalibrationScored, stats.CalibrationTimeout, stats.CalibrationErrors)
	}
}

// No artifact under repoRoot → CalibrationFound stays false.
func TestGather_noCalibrationArtifact(t *testing.T) {
	root := t.TempDir()
	meterDir := filepath.Join(root, ".meter")
	auditDir := filepath.Join(root, ".audit")
	os.MkdirAll(meterDir, 0755)
	os.MkdirAll(auditDir, 0755)

	stats, err := Gather(meterDir, auditDir, root)
	if err != nil {
		t.Fatal(err)
	}
	if stats.CalibrationFound {
		t.Errorf("CalibrationFound=true with no artifact")
	}
}
