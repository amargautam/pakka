package statusline

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amargautam/pakka/internal/hookevent"
)

// mkVersionDirs creates a fake plugin cache layout under a temp root:
//
//	<parent>/<v>/   for each v in versions
//
// and returns the parent dir. The running root is <parent>/<running>.
func mkVersionDirs(t *testing.T, versions ...string) string {
	t.Helper()
	parent := t.TempDir()
	for _, v := range versions {
		if err := os.MkdirAll(filepath.Join(parent, v), 0755); err != nil {
			t.Fatal(err)
		}
	}
	return parent
}

// setPluginRoot points CLAUDE_PLUGIN_ROOT at parent/running and redirects
// OverrideHome to a fresh temp dir so the upgrade cache lands somewhere writable
// and isolated per test.
func setPluginRoot(t *testing.T, parent, running string) {
	t.Helper()
	t.Setenv("CLAUDE_PLUGIN_ROOT", filepath.Join(parent, running))
	prev := OverrideHome
	OverrideHome = t.TempDir()
	t.Cleanup(func() { OverrideHome = prev })
	ResetUpgradeProbes()
}

// AC1: newer sibling present → `↑<highest>`; running highest → no segment.
func TestUpgradeSegment_AC1_NewerAndCurrent(t *testing.T) {
	parent := mkVersionDirs(t, "0.11.0", "0.12.0", "0.12.1", "0.15.1")

	setPluginRoot(t, parent, "0.12.0")
	if got := upgradeVersion(); got != "0.15.1" {
		t.Errorf("running 0.12.0: got %q, want %q", got, "0.15.1")
	}

	setPluginRoot(t, parent, "0.15.1")
	if got := upgradeVersion(); got != "" {
		t.Errorf("running 0.15.1 (highest): got %q, want empty", got)
	}
}

// AC2: numeric per-component comparison; non-semver dirs ignored without error.
func TestUpgradeSegment_AC2_NumericCompareAndIgnoreJunk(t *testing.T) {
	// 0.9.0 must be LESS than 0.15.1 (numeric, not lexicographic). Junk dir names
	// must be ignored, not error.
	parent := mkVersionDirs(t, "0.9.0", "0.15.1", "not-a-version", "v1.2.3", "0.12.0-rc1", "1.2")

	setPluginRoot(t, parent, "0.9.0")
	if got := upgradeVersion(); got != "0.15.1" {
		t.Errorf("numeric compare 0.9.0<0.15.1: got %q, want %q", got, "0.15.1")
	}

	// Running the highest real semver; only junk sits above lexicographically.
	setPluginRoot(t, parent, "0.15.1")
	if got := upgradeVersion(); got != "" {
		t.Errorf("junk siblings must be ignored: got %q, want empty", got)
	}
}

// AC3a: env absent → no segment, no panic.
func TestUpgradeSegment_AC3_NoPluginRoot(t *testing.T) {
	t.Setenv("CLAUDE_PLUGIN_ROOT", "")
	prev := OverrideHome
	OverrideHome = t.TempDir()
	t.Cleanup(func() { OverrideHome = prev })
	if got := upgradeVersion(); got != "" {
		t.Errorf("no plugin root: got %q, want empty", got)
	}
}

// AC3b: cache dir (parent) unreadable/absent → no segment, no error.
func TestUpgradeSegment_AC3_ParentMissing(t *testing.T) {
	parent := t.TempDir()
	// Point at a running root whose parent does not exist.
	setPluginRoot(t, filepath.Join(parent, "ghost"), "0.12.0")
	if got := upgradeVersion(); got != "" {
		t.Errorf("missing parent dir: got %q, want empty", got)
	}
}

// AC3c: root basename not a semver → fall back to plugin.json version; absent →
// no segment.
func TestUpgradeSegment_AC3_PluginJSONFallback(t *testing.T) {
	parent := mkVersionDirs(t, "0.15.1")
	// Running root basename is a non-semver "dev" checkout dir.
	devRoot := filepath.Join(parent, "dev")
	if err := os.MkdirAll(filepath.Join(devRoot, ".claude-plugin"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(devRoot, ".claude-plugin", "plugin.json"),
		[]byte(`{"version":"0.12.0"}`), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_PLUGIN_ROOT", devRoot)
	prev := OverrideHome
	OverrideHome = t.TempDir()
	t.Cleanup(func() { OverrideHome = prev })
	ResetUpgradeProbes()

	if got := upgradeVersion(); got != "0.15.1" {
		t.Errorf("plugin.json fallback (0.12.0 < 0.15.1): got %q, want %q", got, "0.15.1")
	}

	// No plugin.json and non-semver basename → no resolvable running version.
	bareRoot := filepath.Join(parent, "checkout")
	if err := os.MkdirAll(bareRoot, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_PLUGIN_ROOT", bareRoot)
	if got := upgradeVersion(); got != "" {
		t.Errorf("no plugin.json, non-semver basename: got %q, want empty", got)
	}
}

// AC5: parent-dir mtime unchanged across N renders → one readdir; mtime bump →
// re-scan.
func TestUpgradeSegment_AC5_ReaddirCachedByMtime(t *testing.T) {
	parent := mkVersionDirs(t, "0.12.0", "0.15.1")
	setPluginRoot(t, parent, "0.12.0")

	for i := 0; i < 5; i++ {
		if got := upgradeVersion(); got != "0.15.1" {
			t.Fatalf("render %d: got %q, want %q", i, got, "0.15.1")
		}
	}
	if got := UpgradeProbeCount(); got != 1 {
		t.Errorf("5 renders, unchanged mtime: %d readdirs, want 1", got)
	}

	// Add a newer version dir → parent mtime bumps → re-scan.
	if err := os.MkdirAll(filepath.Join(parent, "0.16.0"), 0755); err != nil {
		t.Fatal(err)
	}
	if got := upgradeVersion(); got != "0.16.0" {
		t.Errorf("after adding 0.16.0: got %q, want %q", got, "0.16.0")
	}
	if got := UpgradeProbeCount(); got != 2 {
		t.Errorf("after mtime bump: %d readdirs, want 2", got)
	}
}

// AC1/AC6: the segment is appended to the live Run() status line and the
// tokens/percent body is left intact.
func TestUpgradeSegment_AC6_AdditiveToRunLine(t *testing.T) {
	home := t.TempDir()
	prevHome := OverrideHome
	OverrideHome = home
	prevRepo := OverrideRepoKey
	OverrideRepoKey = func(string) string { return "/repo/x" }
	t.Cleanup(func() { OverrideHome = prevHome; OverrideRepoKey = prevRepo })

	render := func() string {
		var buf bytes.Buffer
		if err := Run(&hookevent.Event{CWD: "/repo/x"}, nil, &buf, "ultra", 0); err != nil {
			t.Fatal(err)
		}
		return buf.String()
	}

	// No plugin root → no upgrade segment, baseline body present.
	t.Setenv("CLAUDE_PLUGIN_ROOT", "")
	base := render()
	if strings.Contains(base, "↑") {
		t.Errorf("no plugin root should not emit upgrade arrow: %q", base)
	}
	if !strings.Contains(base, "saved (est)") || !strings.Contains(base, "bugs caught") {
		t.Errorf("baseline body missing expected segments: %q", base)
	}

	// Newer sibling → segment appended, body still intact.
	parent := mkVersionDirs(t, "0.12.0", "0.15.1")
	t.Setenv("CLAUDE_PLUGIN_ROOT", filepath.Join(parent, "0.12.0"))
	ResetUpgradeProbes()
	withSeg := render()
	if !strings.Contains(withSeg, "0.15.1") {
		t.Errorf("upgrade segment missing from run line: %q", withSeg)
	}
	if !strings.Contains(withSeg, "saved (est)") || !strings.Contains(withSeg, "bugs caught") {
		t.Errorf("run line body altered by upgrade segment: %q", withSeg)
	}
}

// AC6 arrow-glyph gate: the upgrade arrow follows the same UTF-8 gate as the
// separator — "↑" when the locale is UTF-8, ASCII "^" otherwise. Never both.
func TestUpgradeSegment_ArrowUTF8Gate(t *testing.T) {
	prevHome := OverrideHome
	OverrideHome = t.TempDir()
	prevRepo := OverrideRepoKey
	OverrideRepoKey = func(string) string { return "/repo/x" }
	t.Cleanup(func() { OverrideHome = prevHome; OverrideRepoKey = prevRepo })

	parent := mkVersionDirs(t, "0.12.0", "0.15.1")
	t.Setenv("CLAUDE_PLUGIN_ROOT", filepath.Join(parent, "0.12.0"))

	render := func() string {
		ResetUpgradeProbes()
		OverrideHome = t.TempDir() // fresh cache each render to force a scan
		var buf bytes.Buffer
		if err := Run(&hookevent.Event{CWD: "/repo/x"}, nil, &buf, "ultra", 0); err != nil {
			t.Fatal(err)
		}
		return buf.String()
	}

	// UTF-8 locale → "↑0.15.1", no ASCII "^" arrow.
	t.Setenv("LC_ALL", "en_US.UTF-8")
	t.Setenv("LANG", "en_US.UTF-8")
	t.Setenv("LC_CTYPE", "en_US.UTF-8")
	utf8Line := render()
	if !strings.Contains(utf8Line, "↑0.15.1") {
		t.Errorf("UTF-8 locale: want ↑0.15.1, got %q", utf8Line)
	}
	if strings.Contains(utf8Line, "^0.15.1") {
		t.Errorf("UTF-8 locale must not emit ASCII arrow: %q", utf8Line)
	}

	// Non-UTF-8 locale → ASCII "^0.15.1", no "↑".
	t.Setenv("LC_ALL", "C")
	t.Setenv("LANG", "C")
	t.Setenv("LC_CTYPE", "C")
	asciiLine := render()
	if !strings.Contains(asciiLine, "^0.15.1") {
		t.Errorf("non-UTF-8 locale: want ^0.15.1, got %q", asciiLine)
	}
	if strings.Contains(asciiLine, "↑") {
		t.Errorf("non-UTF-8 locale must not emit ↑ arrow: %q", asciiLine)
	}
}

// Unit: numeric semver parse/compare edge cases feeding AC2.
func TestParseSemver(t *testing.T) {
	cases := []struct {
		in string
		ok bool
	}{
		{"0.15.1", true},
		{"0.9.0", true},
		{"10.20.30", true},
		{"1.2", false},
		{"1.2.3.4", false},
		{"v1.2.3", false},
		{"0.12.0-rc1", false},
		{"", false},
		{"a.b.c", false},
		{"1.2.x", false},
	}
	for _, c := range cases {
		if _, ok := parseSemver(c.in); ok != c.ok {
			t.Errorf("parseSemver(%q) ok=%v, want %v", c.in, ok, c.ok)
		}
	}

	a, _ := parseSemver("0.9.0")
	b, _ := parseSemver("0.15.1")
	if !a.less(b) {
		t.Errorf("0.9.0 should be < 0.15.1 (numeric)")
	}
	if b.less(a) {
		t.Errorf("0.15.1 should not be < 0.9.0")
	}
}
