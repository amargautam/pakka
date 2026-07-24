package main

import (
	"os/exec"
	"strings"
	"testing"
)

// forbiddenDeps must never appear in pakka-hot's transitive import graph. Each
// one carries a heavy package-init cost that the PreToolUse/statusLine hot path
// must not pay on every invocation:
//
//   - modernc.org/sqlite (via internal/recall): its modernc.org/libc netdb
//     table build adds ~4ms of init — the original startup-floor regression.
//   - net/http (via internal/compress/semantic's Anthropic client): linking it
//     adds ~1.8ms to the process-startup floor.
//   - internal/compress/semantic + internal/compress/orchestrator: the two
//     packages that would drag the above back in. Guarding them keeps the floor
//     fix from silently regressing if a future edit re-imports them.
//
// See docs/specs/2026-07-24-hot-path-startup-floor.md.
var forbiddenDeps = []string{
	"modernc.org/sqlite",
	"modernc.org/libc",
	"net/http",
	"github.com/amargautam/pakka/internal/recall",
	"github.com/amargautam/pakka/internal/compress/semantic",
	"github.com/amargautam/pakka/internal/compress/orchestrator",
}

// TestPakkaHotHasNoHeavyDeps fails if the lean hot-path binary links any of the
// forbidden heavy dependencies. It reads the actual transitive dependency set
// via `go list -deps` so the guard tracks the real link graph, not a hand list.
func TestPakkaHotHasNoHeavyDeps(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
	out, err := exec.Command("go", "list", "-deps", ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps: %v\n%s", err, out)
	}
	deps := make(map[string]bool)
	for _, line := range strings.Split(string(out), "\n") {
		deps[strings.TrimSpace(line)] = true
	}
	for _, forbidden := range forbiddenDeps {
		if deps[forbidden] {
			t.Errorf("pakka-hot must NOT link %q — it reintroduces the hot-path "+
				"startup-floor cost this binary exists to avoid "+
				"(see docs/specs/2026-07-24-hot-path-startup-floor.md)", forbidden)
		}
	}
}
