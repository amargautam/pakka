package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/amargautam/pakka/internal/commitgate"
)

// ReviewPassCmd implements the "review-pass" subcommand.
//
// It records a diff-bound review pass: it hashes the current staged diff and
// writes a JSON marker {ts, diffSHA256, verdict:"passed"} to
// <repo-root>/.pakka/reviews/last-pass-ts. The commit gate later re-hashes the
// staged diff and authorizes the commit only when the hash still matches, so a
// pass cannot authorize a different set of changes.
//
// Review flows call this instead of shell-redirecting a bare epoch.
type ReviewPassCmd struct{}

func (c *ReviewPassCmd) Name() string { return "review-pass" }

// Run computes the staged-diff hash and writes the marker. An empty staged
// diff is an error (exit nonzero, no marker written): there is nothing to bind
// a pass to.
//
// --repo-root pins the repo whose staged diff is hashed and whose marker is
// written. It MUST be supplied when the reviewed repo is not the process CWD's
// git root — otherwise writer and gate disagree, since the gate resolves the
// repo from the commit command's -C/cd path. Default: git root of CWD.
func (c *ReviewPassCmd) Run(args []string) error {
	fs := flag.NewFlagSet("review-pass", flag.ContinueOnError)
	repoRootFlag := fs.String("repo-root", "", "repo root whose staged diff is hashed (default: git root of CWD)")
	findingsFlag := fs.String("findings", "", "path to the review findings JSONL to bind to the marker (optional)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	root := *repoRootFlag
	if root == "" {
		root = repoRoot()
	}

	marker, err := recordReviewPass(root, *findingsFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pakka: review-pass: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("pakka: review pass recorded (diffSHA256=%s)\n", marker.DiffSHA256[:12])
	return nil
}

// recordReviewPass hashes the staged diff of the repo at root and writes the
// diff-bound pass marker. Returns an error (and writes no marker) when the
// staged diff is empty or cannot be read/written. Extracted from Run so tests
// can exercise it without os.Exit.
//
// When findingsPath is non-empty, the findings file is read and hashed before
// the marker is written: the marker gains findingsSHA256 (sha256 of the file
// bytes), findingsCounts (severity tally), and findingsPath (stored relative
// to the git toplevel so the gate can re-resolve it). An unreadable findings
// path is an error and no marker is written — a pass cannot bind to evidence
// that does not exist.
func recordReviewPass(root, findingsPath string) (commitgate.PassMarker, error) {
	diff, err := stagedDiff(root)
	if err != nil {
		return commitgate.PassMarker{}, fmt.Errorf("cannot read staged diff: %w", err)
	}
	if len(diff) == 0 {
		return commitgate.PassMarker{}, fmt.Errorf("no staged changes — stage the reviewed diff before recording a pass")
	}

	// The marker path MUST equal the gate's read path, which is
	// <git-toplevel>/.pakka/reviews (resolveReviewsDir). Normalize root to the
	// git toplevel so a subdir --repo-root (or a CWD below the root) still
	// writes where the gate looks.
	probe := root
	if probe == "" {
		probe = "."
	}
	top := repoRootAt(probe)
	reviewsDir := ".pakka/reviews"
	if top != "" {
		reviewsDir = filepath.Join(top, ".pakka", "reviews")
	} else if root != "" {
		reviewsDir = filepath.Join(root, ".pakka", "reviews")
	}

	marker := commitgate.PassMarker{
		TS:         time.Now().Unix(),
		DiffSHA256: sha256Hex(diff),
		Verdict:    "passed",
	}

	// Bind the review evidence, if supplied. Read + hash BEFORE writing the
	// marker so an unreadable findings path leaves no marker on disk.
	if findingsPath != "" {
		fdata, err := os.ReadFile(findingsPath)
		if err != nil {
			return commitgate.PassMarker{}, fmt.Errorf("cannot read findings file %s: %w", findingsPath, err)
		}
		counts := tallyFindings(fdata)
		marker.FindingsSHA256 = sha256Hex(fdata)
		marker.FindingsCounts = &counts
		marker.FindingsPath = relFindingsPath(top, findingsPath)
	}

	if err := writePassMarker(reviewsDir, marker); err != nil {
		return commitgate.PassMarker{}, fmt.Errorf("cannot write marker: %w", err)
	}
	return marker, nil
}

// tallyFindings parses a findings JSONL blob and tallies severities. Each row
// that parses as JSON counts toward Total; rows whose severity is "error",
// "warn"/"warning", or "info" also increment the matching bucket. The reviewer
// agents emit "warn" (see agents/*.md); "warning" is accepted as an alias so
// hand-written or legacy findings tally the same. Unknown or absent severities
// count toward Total only. Unparseable lines are ignored.
func tallyFindings(data []byte) commitgate.FindingsCounts {
	var c commitgate.FindingsCounts
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var row struct {
			Severity string `json:"severity"`
		}
		if json.Unmarshal([]byte(line), &row) != nil {
			continue
		}
		c.Total++
		switch row.Severity {
		case "error":
			c.Error++
		case "warn", "warning":
			c.Warning++
		case "info":
			c.Info++
		}
	}
	return c
}

// relFindingsPath renders findingsPath relative to the git toplevel top so the
// gate can resolve it from the repo root. Falls back to the original path when
// top is unknown or a relative path cannot be computed.
func relFindingsPath(top, findingsPath string) string {
	if top == "" {
		return findingsPath
	}
	abs, err := filepath.Abs(findingsPath)
	if err != nil {
		return findingsPath
	}
	rel, err := filepath.Rel(top, abs)
	if err != nil {
		return findingsPath
	}
	return rel
}

// extractRationales concatenates the "rationale" text of every parsed finding
// row into a single space-separated string, so the audit "review-verdict"
// entry carries the findings' prose for FTS5 to match.
func extractRationales(data []byte) string {
	var parts []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var row struct {
			Rationale string `json:"rationale"`
		}
		if json.Unmarshal([]byte(line), &row) != nil {
			continue
		}
		if row.Rationale != "" {
			parts = append(parts, row.Rationale)
		}
	}
	return strings.Join(parts, " ")
}

// stagedDiff returns the raw bytes of `git diff --cached` for the repo rooted
// at root (or the process CWD repo when root is ""). The bytes are hashed
// verbatim, so both writer and gate must invoke git identically.
func stagedDiff(root string) ([]byte, error) {
	args := []string{}
	if root != "" {
		args = append(args, "-C", root)
	}
	args = append(args, "diff", "--cached")
	return exec.Command("git", args...).Output()
}

// sha256Hex returns the lowercase hex sha256 of b.
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// writePassMarker writes the marker JSON atomically to
// <reviewsDir>/last-pass-ts via a temp file + rename, so a concurrent gate
// read never sees a partially written marker.
func writePassMarker(reviewsDir string, marker commitgate.PassMarker) error {
	if err := os.MkdirAll(reviewsDir, 0755); err != nil {
		return err
	}
	data, err := json.Marshal(marker)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(reviewsDir, "last-pass-ts-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, filepath.Join(reviewsDir, "last-pass-ts")); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
