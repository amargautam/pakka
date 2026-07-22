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
	if err := fs.Parse(args); err != nil {
		return err
	}

	root := *repoRootFlag
	if root == "" {
		root = repoRoot()
	}

	marker, err := recordReviewPass(root)
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
func recordReviewPass(root string) (commitgate.PassMarker, error) {
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
	if err := writePassMarker(reviewsDir, marker); err != nil {
		return commitgate.PassMarker{}, fmt.Errorf("cannot write marker: %w", err)
	}
	return marker, nil
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
