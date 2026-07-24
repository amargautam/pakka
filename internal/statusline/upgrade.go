// Upgrade visibility — surface a newer cached plugin version in the status line.
//
// Plugin upgrades are otherwise silent: a session keeps enforcing with an old
// cached version while a newer one sits beside it in the local plugin cache.
// pakka may not dial home, but the cache directory already knows a newer version
// exists — this reads it (pure readdir, zero network) and appends a compact
// `↑<version>` segment to the live status line.
//
// Signal source: the sibling version directories of the running plugin root.
// CLAUDE_PLUGIN_ROOT points at
//
//	~/.claude/plugins/cache/pakka-marketplace/pakka/<version>/
//
// so the running version is the root's basename and the candidate newer
// versions are the other subdirs of the root's parent. When the basename is not
// a semver (dev checkout, symlinked root), the running version falls back to the
// "version" field of plugin.json under the root; if that too is unavailable the
// segment is omitted entirely.
//
// Hot path: this runs on every status-line render, so the readdir result is
// cached by the parent dir's mtime (mirroring meterCache / resolveCache). The
// parent dir's mtime bumps when a version dir is added or removed — exactly when
// a re-scan is warranted; unchanged → zero readdir.
package statusline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// upgradeProbes counts, process-wide, how many times the sibling version
// directory was read from scratch (a real os.ReadDir) rather than served from
// the mtime-keyed cache. Tests read it to prove that N renders over an unchanged
// cache dir do the readdir exactly once. Not concurrency-safe; the status line
// renders on a single goroutine per process.
var upgradeProbes int

// UpgradeProbeCount returns the number of from-scratch sibling-dir readdirs
// performed since the last ResetUpgradeProbes. Test-only observability hook.
func UpgradeProbeCount() int { return upgradeProbes }

// ResetUpgradeProbes zeroes the upgrade-probe counter. Test-only.
func ResetUpgradeProbes() { upgradeProbes = 0 }

// semver is a parsed MAJOR.MINOR.PATCH triple.
type semver [3]int

// parseSemver parses a strict numeric "MAJOR.MINOR.PATCH" string. Any name that
// is not exactly three dot-separated all-digit components (prerelease suffixes,
// "v" prefixes, non-numeric junk) is rejected with ok=false and ignored by
// callers — never an error.
func parseSemver(s string) (semver, bool) {
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return semver{}, false
	}
	var v semver
	for i, p := range parts {
		if p == "" {
			return semver{}, false
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return semver{}, false
			}
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return semver{}, false
		}
		v[i] = n
	}
	return v, true
}

// less reports whether a < b, compared numerically component by component.
func (a semver) less(b semver) bool {
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// pluginJSON is the subset of .claude-plugin/plugin.json the upgrade check reads.
type pluginJSON struct {
	Version string `json:"version"`
}

// versionFromPluginJSON reads the running version from plugin.json under root
// when the root basename is not itself a semver (dev checkout, symlinked root).
// It checks the canonical .claude-plugin/plugin.json first, then a bare
// plugin.json at the root. Returns ok=false when neither exists or neither
// carries a parseable semver.
func versionFromPluginJSON(root string) (semver, bool) {
	for _, rel := range []string{
		filepath.Join(".claude-plugin", "plugin.json"),
		"plugin.json",
	} {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		var pj pluginJSON
		if json.Unmarshal(data, &pj) != nil {
			continue
		}
		if v, ok := parseSemver(pj.Version); ok {
			return v, true
		}
	}
	return semver{}, false
}

// upgradeCache memoizes the max sibling version by the parent dir's mtime,
// mirroring meterCache/resolveCache. A single entry suffices: a process renders
// one plugin root's status line, so the parent dir is stable within a process.
type upgradeCache struct {
	ParentDir string `json:"parent_dir"`
	Mtime     int64  `json:"mtime"`
	MaxVer    string `json:"max_ver"` // dir name of the highest sibling; "" if none
}

func loadUpgradeCache(path string) *upgradeCache {
	c := &upgradeCache{}
	data, err := os.ReadFile(path)
	if err != nil {
		return c
	}
	_ = json.Unmarshal(data, c)
	return c
}

func saveUpgradeCache(path string, c *upgradeCache) {
	data, err := json.Marshal(c)
	if err != nil {
		return
	}
	// Ensure ~/.pakka exists — on a fresh install (or in tests) the cache dir may
	// not exist yet, and a silent write failure would defeat the cache (every
	// render would re-scan the sibling dir).
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

// scanMaxSibling reads parentDir and returns the directory name with the highest
// parseable semver. Non-directory entries and non-semver names are ignored.
// Returns "" when the dir is unreadable or holds no semver-named subdir. Bumps
// upgradeProbes exactly once per call — this is the single readdir the cache
// exists to elide.
func scanMaxSibling(parentDir string) string {
	upgradeProbes++
	entries, err := os.ReadDir(parentDir)
	if err != nil {
		return ""
	}
	var maxName string
	var maxVer semver
	have := false
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		v, ok := parseSemver(e.Name())
		if !ok {
			continue
		}
		if !have || maxVer.less(v) {
			maxVer, maxName, have = v, e.Name(), true
		}
	}
	return maxName
}

// upgradeVersion returns the highest cached plugin version newer than the
// running one (bare, e.g. "0.15.1"), or "" when the running version is current
// or the signal is unavailable (no plugin root, unreadable cache dir,
// unresolvable running version). The caller prepends the arrow glyph so it can
// honor the terminal's UTF-8 gate ("↑" vs ASCII "^"). Never returns an error
// and never performs any network I/O.
func upgradeVersion() string {
	root := os.Getenv("CLAUDE_PLUGIN_ROOT")
	if root == "" {
		return ""
	}

	running, ok := parseSemver(filepath.Base(root))
	if !ok {
		running, ok = versionFromPluginJSON(root)
		if !ok {
			return ""
		}
	}

	parent := filepath.Dir(root)
	info, err := os.Stat(parent)
	if err != nil {
		return ""
	}
	mtime := info.ModTime().UnixNano()

	cachePath := filepath.Join(resolveHome(), ".pakka", "upgrade-cache.json")
	cache := loadUpgradeCache(cachePath)

	var maxName string
	if cache.ParentDir == parent && cache.Mtime == mtime {
		maxName = cache.MaxVer
	} else {
		maxName = scanMaxSibling(parent)
		saveUpgradeCache(cachePath, &upgradeCache{ParentDir: parent, Mtime: mtime, MaxVer: maxName})
	}
	if maxName == "" {
		return ""
	}

	maxVer, ok := parseSemver(maxName)
	if !ok {
		return ""
	}
	if running.less(maxVer) {
		return maxName
	}
	return ""
}
