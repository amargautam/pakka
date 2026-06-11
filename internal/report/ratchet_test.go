package report

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// publishedOutputTokensFloor is the ratchet floor for the published
// output-tokens proof number in RECEIPTS.md.
//
// Re-based 2026-06-11 for #10: the figure is now the MAX session-end
// snapshot among entries whose canonical repo_root tag falls under the
// pakka.dev workspace (workspace-root tags contain child repos) — not the
// v0.9.0 sum-over-transcripts figure (5,939,566), which double-counted
// cumulative snapshots and is superseded. Regenerated locally via
// `pakka-core report --repo-root=/Users/amar/Projects/pakka.dev` after
// running the repo_root backfill. Snapshots are cumulative, so the max can
// only grow: any future regeneration must publish >= this floor. Raise the
// floor when the published number is regenerated; never lower it.
const publishedOutputTokensFloor = 1_019_833

// TestPublishedOutputTokensRatchet asserts the published RECEIPTS.md figure
// never regresses below the regenerated floor (monotonic ratchet).
func TestPublishedOutputTokensRatchet(t *testing.T) {
	got := publishedOutputTokens(t)
	if got < publishedOutputTokensFloor {
		t.Errorf("RECEIPTS.md output tokens = %d, below ratchet floor %d — published proof number must be monotonic (max cumulative snapshot only grows)",
			got, publishedOutputTokensFloor)
	}
}

// TestOutputTokensFigureIsMonotonicMaxSnapshot pins the semantics behind the
// ratchet: the figure is the max snapshot, so appending an OLDER/SMALLER
// snapshot never lowers it, and appending a larger one raises it. This is
// what makes the published number safe to ratchet.
func TestOutputTokensFigureIsMonotonicMaxSnapshot(t *testing.T) {
	tmp := t.TempDir()
	meterDir := filepath.Join(tmp, "meter")
	auditDir := filepath.Join(tmp, "audit")
	for _, d := range []string{meterDir, auditDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", tmp)

	repo := canonicalize(t, tmp)
	write := func(lines string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(meterDir, "s.jsonl"), []byte(lines), 0644); err != nil {
			t.Fatal(err)
		}
	}
	figure := func() int64 {
		t.Helper()
		stats, err := Gather(meterDir, auditDir, tmp)
		if err != nil {
			t.Fatal(err)
		}
		return stats.OutputTokensTotal
	}

	base := `{"ts":"2026-06-01T10:00:00Z","session_id":"s1","repo":"` + repo + `","output_tokens":5000}
`
	write(base)
	if got := figure(); got != 5000 {
		t.Fatalf("baseline figure = %d, want 5000", got)
	}

	// A smaller snapshot (stale session flushing late) must NOT lower the figure.
	write(base + `{"ts":"2026-06-02T10:00:00Z","session_id":"s2","repo":"` + repo + `","output_tokens":300}
`)
	if got := figure(); got != 5000 {
		t.Errorf("figure after smaller snapshot = %d, want 5000 (monotonic: max never decreases)", got)
	}

	// A larger snapshot raises it.
	write(base + `{"ts":"2026-06-03T10:00:00Z","session_id":"s3","repo":"` + repo + `","output_tokens":7500}
`)
	if got := figure(); got != 7500 {
		t.Errorf("figure after larger snapshot = %d, want 7500 (max snapshot)", got)
	}
}

// publishedOutputTokens parses the "Output tokens measured across N
// sessions: X" line from the repo's RECEIPTS.md.
func publishedOutputTokens(t *testing.T) int64 {
	t.Helper()
	path := filepath.Join("..", "..", "RECEIPTS.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("RECEIPTS.md not readable: %v", err)
	}
	re := regexp.MustCompile(`Output tokens measured across [\d,]+ sessions: ([\d,]+)`)
	m := re.FindStringSubmatch(string(data))
	if m == nil {
		t.Fatalf("RECEIPTS.md missing 'Output tokens measured' line — ratchet has nothing to check")
	}
	n, err := strconv.ParseInt(strings.ReplaceAll(m[1], ",", ""), 10, 64)
	if err != nil {
		t.Fatalf("parse published output tokens %q: %v", m[1], err)
	}
	return n
}
