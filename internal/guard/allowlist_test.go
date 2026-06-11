package guard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/amargautam/pakka/internal/hookevent"
)

// newRepo creates a temp dir with a .git marker so RepoRoot resolves to it.
func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func bashEvent(cmd, cwd string) *hookevent.Event {
	input, _ := json.Marshal(map[string]string{"command": cmd})
	return &hookevent.Event{
		ToolName:  "Bash",
		ToolInput: json.RawMessage(input),
		CWD:       cwd,
	}
}

func allowlistPath(root string) string {
	return filepath.Join(root, ".pakka", "guard-allowlist.json")
}

// --- shape normalization ---

func TestShapeCollapsesWhitespaceAndVariesWithInput(t *testing.T) {
	if got := Shape("ls   ../../x\n"); got != "ls ../../x" {
		t.Errorf("Shape() = %q, want %q", got, "ls ../../x")
	}
	if Shape("ls ../../a") == Shape("ls ../../b") {
		t.Error("Shape must vary with command content")
	}
}

// --- repo root resolution ---

func TestRepoRootFindsGitDir(t *testing.T) {
	repo := newRepo(t)
	sub := filepath.Join(repo, "a", "b")
	os.MkdirAll(sub, 0755)
	if got := RepoRoot(sub); got != repo {
		t.Errorf("RepoRoot(%q) = %q, want %q", sub, got, repo)
	}
}

func TestRepoRootEmptyCWD(t *testing.T) {
	if got := RepoRoot(""); got != "" {
		t.Errorf("RepoRoot(\"\") = %q, want empty", got)
	}
}

func TestRepoRootSkipsHome(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	// Home itself has .git (dotfiles repo); cwd below home with no .git.
	os.MkdirAll(filepath.Join(tmp, ".git"), 0755)
	sub := filepath.Join(tmp, "scratch")
	os.MkdirAll(sub, 0755)
	if got := RepoRoot(sub); got == filepath.Clean(tmp) {
		t.Errorf("RepoRoot must not resolve to $HOME, got %q", got)
	}
}

// --- the core loop: blocked → override recorded → same command passes ---

func TestOverrideLoopBlockedRecordedThenPasses(t *testing.T) {
	repo := newRepo(t)
	cmd := "ls ../../sibling/dir"
	cfg := DefaultConfig()

	// 1. Blocked, allowlistable, pattern + shape surfaced.
	r := RunWithConfig(bashEvent(cmd, repo), cfg)
	if r.Allowed {
		t.Fatal("expected initial block")
	}
	if !r.Allowlistable {
		t.Fatal("traversal block should be allowlistable")
	}
	if r.Pattern != "traversal" {
		t.Fatalf("Pattern = %q, want traversal", r.Pattern)
	}
	if r.Shape != Shape(cmd) {
		t.Fatalf("Shape = %q, want %q", r.Shape, Shape(cmd))
	}
	if r.RepoRoot != repo {
		t.Fatalf("RepoRoot = %q, want %q", r.RepoRoot, repo)
	}

	// 2. User override recorded.
	if err := RecordOverride(repo, r.Pattern, r.Shape, cfg, time.Now()); err != nil {
		t.Fatal(err)
	}

	// 3. Identical command now passes, attributed to the pattern.
	r2 := RunWithConfig(bashEvent(cmd, repo), cfg)
	if !r2.Allowed {
		t.Fatalf("expected allow after override, got block: %s", r2.Reason)
	}
	if r2.AllowlistedBy != "traversal" {
		t.Fatalf("AllowlistedBy = %q, want traversal", r2.AllowlistedBy)
	}

	// 4. A different command stays blocked — no over-generalization.
	r3 := RunWithConfig(bashEvent("ls ../../other/dir", repo), cfg)
	if r3.Allowed {
		t.Fatal("different command must stay blocked")
	}

	// 5. Same repo allowlist has no effect in another repo.
	other := newRepo(t)
	r4 := RunWithConfig(bashEvent(cmd, other), cfg)
	if r4.Allowed {
		t.Fatal("allowlist must be per-repo; other repo should still block")
	}
}

func TestOverrideCountVariesWithRecordings(t *testing.T) {
	repo := newRepo(t)
	cfg := DefaultConfig()
	shape := Shape("ls ../../x")
	for i := 0; i < 3; i++ {
		if err := RecordOverride(repo, "traversal", shape, cfg, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	al, err := Load(repo)
	if err != nil || al == nil {
		t.Fatalf("Load: al=%v err=%v", al, err)
	}
	if got := al.Patterns["traversal"].Shapes[shape].Count; got != 3 {
		t.Errorf("Count = %d, want 3", got)
	}
}

// --- non-allowlistable categories ---

func TestSystemPathStaysBlockedEvenWhenListed(t *testing.T) {
	repo := newRepo(t)
	cmd := "cat /etc/passwd"
	// Adversarial: hand-craft an allowlist naming the non-allowlistable category.
	os.MkdirAll(filepath.Join(repo, ".pakka"), 0755)
	raw := `{"schema":"pakka.guard-allowlist.v1","patterns":{"system-path":{"mode":"warn","shapes":{"` +
		Shape(cmd) + `":{"count":99,"first_ts":"2026-06-01T00:00:00Z","last_ts":"2026-06-11T00:00:00Z"}}}}}`
	os.WriteFile(allowlistPath(repo), []byte(raw), 0644)

	r := RunWithConfig(bashEvent(cmd, repo), DefaultConfig())
	if r.Allowed {
		t.Fatal("system-path must never be allowlistable")
	}
	if r.Allowlistable {
		t.Error("system-path block must not offer override")
	}
}

func TestRecordOverrideRejectsNonAllowlistablePattern(t *testing.T) {
	repo := newRepo(t)
	if err := RecordOverride(repo, "system-path", "cat /etc/passwd", DefaultConfig(), time.Now()); err == nil {
		t.Fatal("RecordOverride must reject non-allowlistable patterns")
	}
}

func TestSecretsPathsNeverConsultAllowlist(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	os.MkdirAll(filepath.Join(tmp, ".ssh"), 0700)
	os.WriteFile(filepath.Join(tmp, ".ssh", "id_rsa"), []byte("k"), 0600)

	repo := newRepo(t)
	// Allowlist that adversarially claims everything is fine.
	os.MkdirAll(filepath.Join(repo, ".pakka"), 0755)
	raw := `{"schema":"pakka.guard-allowlist.v1","patterns":{"traversal":{"mode":"warn","shapes":{}}}}`
	os.WriteFile(allowlistPath(repo), []byte(raw), 0644)

	input, _ := json.Marshal(map[string]string{"file_path": filepath.Join(tmp, ".ssh", "id_rsa")})
	r := RunWithConfig(&hookevent.Event{ToolName: "Read", ToolInput: input, CWD: repo}, DefaultConfig())
	if r.Allowed {
		t.Fatal("SSH key read must stay blocked regardless of allowlist")
	}

	envInput, _ := json.Marshal(map[string]string{"file_path": ".env"})
	r2 := RunWithConfig(&hookevent.Event{ToolName: "Read", ToolInput: envInput, CWD: repo}, DefaultConfig())
	if r2.Allowed {
		t.Fatal(".env read must stay blocked regardless of allowlist")
	}
}

func TestAllowlistedShapeStillBlockedByNonAllowlistablePattern(t *testing.T) {
	repo := newRepo(t)
	cfg := DefaultConfig()
	// Command matches eval (allowlistable, checked first) AND system-path.
	cmd := "eval $(cat /etc/passwd)"
	if err := RecordOverride(repo, "eval", Shape(cmd), cfg, time.Now()); err != nil {
		t.Fatal(err)
	}
	r := RunWithConfig(bashEvent(cmd, repo), cfg)
	if r.Allowed {
		t.Fatal("allowlisted eval shape must not bypass system-path deny")
	}
}

// --- decay / demotion ---

func TestRecordOverrideDemotesAtThresholdNotBelow(t *testing.T) {
	cfg := Config{DemoteThreshold: 5, DecayWindowDays: 30}

	below := newRepo(t)
	for i := 0; i < 4; i++ {
		if err := RecordOverride(below, "traversal", Shape("cmd variant "+string(rune('a'+i))+" ../../x"), cfg, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	al, _ := Load(below)
	if al.Patterns["traversal"].Mode != "block" {
		t.Fatalf("below threshold: mode = %q, want block", al.Patterns["traversal"].Mode)
	}
	// Novel traversal command still blocked below threshold.
	if r := RunWithConfig(bashEvent("ls ../../novel", below), cfg); r.Allowed {
		t.Fatal("novel command must stay blocked below threshold")
	}

	at := newRepo(t)
	for i := 0; i < 5; i++ {
		if err := RecordOverride(at, "traversal", Shape("cmd variant "+string(rune('a'+i))+" ../../x"), cfg, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	al2, _ := Load(at)
	if al2.Patterns["traversal"].Mode != "warn" {
		t.Fatalf("at threshold: mode = %q, want warn", al2.Patterns["traversal"].Mode)
	}
	// Novel traversal command now passes with a warn verdict.
	r := RunWithConfig(bashEvent("ls ../../novel", at), cfg)
	if !r.Allowed {
		t.Fatalf("demoted pattern should warn-allow, got block: %s", r.Reason)
	}
	if !r.Warned || r.Pattern != "traversal" {
		t.Errorf("want Warned=true Pattern=traversal, got Warned=%v Pattern=%q", r.Warned, r.Pattern)
	}
}

func TestMaintainDecayExpiresOldOverridesAndRepromotes(t *testing.T) {
	cfg := Config{DemoteThreshold: 5, DecayWindowDays: 30}
	now := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -40).Format(time.RFC3339)

	al := &Allowlist{
		Schema: SchemaV1,
		Patterns: map[string]*PatternEntry{
			"traversal": {
				Mode: "warn",
				Shapes: map[string]*ShapeEntry{
					"ls ../../x": {Count: 9, FirstTS: old, LastTS: old},
				},
			},
		},
	}
	if !MaintainDecay(al, cfg, now) {
		t.Fatal("decay should report change for expired overrides")
	}
	if pe := al.Patterns["traversal"]; pe != nil && pe.Mode == "warn" {
		t.Error("expired overrides must re-promote warn → block")
	}
}

func TestMaintainDecayDemotesRecentAboveThreshold(t *testing.T) {
	cfg := Config{DemoteThreshold: 5, DecayWindowDays: 30}
	now := time.Now()
	recent := now.AddDate(0, 0, -1).Format(time.RFC3339)
	al := &Allowlist{
		Schema: SchemaV1,
		Patterns: map[string]*PatternEntry{
			"traversal": {
				Mode: "block",
				Shapes: map[string]*ShapeEntry{
					"ls ../../x": {Count: 6, FirstTS: recent, LastTS: recent},
				},
			},
		},
	}
	if !MaintainDecay(al, cfg, now) {
		t.Fatal("decay should report change")
	}
	if al.Patterns["traversal"].Mode != "warn" {
		t.Errorf("mode = %q, want warn", al.Patterns["traversal"].Mode)
	}
}

func TestExpiredOverridesStopMatchingAtConsult(t *testing.T) {
	// Decay applies in memory on every consult: a shape recorded 40 days ago
	// must not allow the command today, even though the file still lists it.
	repo := newRepo(t)
	cfg := DefaultConfig()
	cmd := "ls ../../sibling/dir"
	old := time.Now().AddDate(0, 0, -40)
	if err := RecordOverride(repo, "traversal", Shape(cmd), cfg, old); err != nil {
		t.Fatal(err)
	}
	r := RunWithConfig(bashEvent(cmd, repo), cfg)
	if r.Allowed {
		t.Fatal("override outside decay window must not match")
	}
}

// --- malformed allowlist: fail closed ---

func TestMalformedAllowlistFailsClosed(t *testing.T) {
	repo := newRepo(t)
	cfg := DefaultConfig()
	cmd := "ls ../../sibling/dir"

	// Record a valid override first, then corrupt the file.
	if err := RecordOverride(repo, "traversal", Shape(cmd), cfg, time.Now()); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(allowlistPath(repo), []byte("{not json"), 0644)

	r := RunWithConfig(bashEvent(cmd, repo), cfg)
	if r.Allowed {
		t.Fatal("malformed allowlist must fail closed (block)")
	}
	if r.AllowlistErr == "" {
		t.Error("malformed allowlist must surface an error for audit logging")
	}
	if r.Allowlistable {
		t.Error("malformed allowlist must hard-block, not offer override")
	}

	// RecordOverride must not clobber the malformed file.
	if err := RecordOverride(repo, "traversal", Shape(cmd), cfg, time.Now()); err == nil {
		t.Fatal("RecordOverride must refuse to write over a malformed allowlist")
	}
	b, _ := os.ReadFile(allowlistPath(repo))
	if string(b) != "{not json" {
		t.Error("malformed allowlist file must be left untouched")
	}
}

func TestUnknownSchemaFailsClosed(t *testing.T) {
	repo := newRepo(t)
	os.MkdirAll(filepath.Join(repo, ".pakka"), 0755)
	os.WriteFile(allowlistPath(repo), []byte(`{"schema":"pakka.guard-allowlist.v99","patterns":{}}`), 0644)
	if _, err := Load(repo); err == nil {
		t.Fatal("unknown schema must be treated as malformed")
	}
}

// --- allowlist file write-protection ---

func TestWriteToolCannotTouchAllowlist(t *testing.T) {
	repo := newRepo(t)
	for _, tool := range []string{"Write", "Edit", "MultiEdit"} {
		input, _ := json.Marshal(map[string]string{"file_path": allowlistPath(repo)})
		r := Run(&hookevent.Event{ToolName: tool, ToolInput: input, CWD: repo})
		if r.Allowed {
			t.Errorf("%s to guard-allowlist.json must be blocked", tool)
		}
	}
	// Relative path form.
	input, _ := json.Marshal(map[string]string{"file_path": ".pakka/guard-allowlist.json"})
	if r := Run(&hookevent.Event{ToolName: "Write", ToolInput: input, CWD: repo}); r.Allowed {
		t.Error("relative Write to guard-allowlist.json must be blocked")
	}
}

func TestReadOfAllowlistStaysAllowed(t *testing.T) {
	repo := newRepo(t)
	input, _ := json.Marshal(map[string]string{"file_path": allowlistPath(repo)})
	if r := Run(&hookevent.Event{ToolName: "Read", ToolInput: input, CWD: repo}); !r.Allowed {
		t.Errorf("reading the allowlist must stay allowed (transparency): %s", r.Reason)
	}
}

func TestBashTamperWithAllowlistBlocked(t *testing.T) {
	blocked := []string{
		`echo '{}' > .pakka/guard-allowlist.json`,
		`echo x >> .pakka/guard-allowlist.json`,
		`printf '{}' | tee .pakka/guard-allowlist.json`,
		`cp /tmp/evil.json .pakka/guard-allowlist.json`,
		`mv .pakka/guard-allowlist.json /tmp/`,
		`sed -i '' 's/block/warn/' .pakka/guard-allowlist.json`,
		// Default deny: write primitives beyond the classic redirect family.
		`python3 -c "open('.pakka/guard-allowlist.json','w').write('{}')"`,
		`dd if=/tmp/evil.json of=.pakka/guard-allowlist.json`,
		`ln -sf /tmp/evil.json .pakka/guard-allowlist.json`,
		`truncate -s 0 .pakka/guard-allowlist.json`,
		`perl -i -pe 's/block/warn/' .pakka/guard-allowlist.json`,
		`install -m 644 /tmp/evil.json .pakka/guard-allowlist.json`,
		// Read-only tool but piped/chained — still denied.
		`cat .pakka/guard-allowlist.json; rm -f x`,
		`cat /tmp/evil.json > .pakka/guard-allowlist.json`,
	}
	for _, cmd := range blocked {
		r := Run(bashEvent(cmd, "/tmp/repo"))
		if r.Allowed {
			t.Errorf("tamper command must be blocked: %q", cmd)
			continue
		}
		if r.Allowlistable {
			t.Errorf("tamper block must not be allowlistable: %q", cmd)
		}
	}
	allowed := []string{
		`cat .pakka/guard-allowlist.json`,
		`jq . .pakka/guard-allowlist.json`,
	}
	for _, cmd := range allowed {
		if r := Run(bashEvent(cmd, "/tmp/repo")); !r.Allowed {
			t.Errorf("read-only command must stay allowed: %q (%s)", cmd, r.Reason)
		}
	}
}

func TestBypassPermissionModesGetNoAsk(t *testing.T) {
	repo := newRepo(t)
	for _, mode := range []string{"bypassPermissions", "dontAsk"} {
		ev := bashEvent("ls ../../sibling/dir", repo)
		ev.PermissionMode = mode
		r := RunWithConfig(ev, DefaultConfig())
		if r.Allowed {
			t.Fatalf("mode %s: expected block", mode)
		}
		if r.Allowlistable {
			t.Errorf("mode %s: ask would auto-approve — must hard-block, not offer override", mode)
		}
	}
}

// --- pending override markers ---

func TestPendingOverrideRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Now()
	hash := InputHash([]byte(`{"command":"ls ../../x"}`))

	if err := WritePendingOverride(hash, PendingOverride{
		Pattern: "traversal", Shape: "ls ../../x", Root: "/tmp/repo", SessionID: "sess-1",
	}, now); err != nil {
		t.Fatal(err)
	}

	po, ok := ConsumeOverride(hash, "sess-1", now.Add(time.Minute))
	if !ok {
		t.Fatal("expected marker consumed")
	}
	if po.Pattern != "traversal" || po.Shape != "ls ../../x" || po.Root != "/tmp/repo" {
		t.Errorf("marker fields wrong: %+v", po)
	}

	// Consumed once — gone.
	if _, ok := ConsumeOverride(hash, "sess-1", now.Add(time.Minute)); ok {
		t.Error("marker must be single-use")
	}
}

func TestPendingOverrideSessionMismatchNotConsumed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Now()
	hash := InputHash([]byte(`{"command":"x"}`))
	WritePendingOverride(hash, PendingOverride{Pattern: "traversal", Shape: "x", Root: "/r", SessionID: "sess-1"}, now)
	if _, ok := ConsumeOverride(hash, "sess-other", now); ok {
		t.Error("marker from another session must not be consumed")
	}
}

func TestPendingOverrideExpires(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Now()
	hash := InputHash([]byte(`{"command":"y"}`))
	WritePendingOverride(hash, PendingOverride{Pattern: "traversal", Shape: "y", Root: "/r", SessionID: "sess-1"}, now)
	if _, ok := ConsumeOverride(hash, "sess-1", now.Add(time.Hour)); ok {
		t.Error("stale marker (>30m) must not be consumed")
	}
}

func TestPendingSweepRemovesStaleMarkers(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Now()
	staleHash := InputHash([]byte(`{"command":"stale"}`))
	WritePendingOverride(staleHash, PendingOverride{Pattern: "traversal", Shape: "stale", Root: "/r", SessionID: "s"}, now)
	dir, _ := pendingDir()
	stalePath := filepath.Join(dir, staleHash+".json")
	old := now.Add(-2 * time.Hour)
	if err := os.Chtimes(stalePath, old, old); err != nil {
		t.Fatal(err)
	}

	WritePendingOverride(InputHash([]byte(`{"command":"fresh"}`)), PendingOverride{Pattern: "traversal", Shape: "fresh", Root: "/r", SessionID: "s"}, now)
	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Error("stale marker should be swept on next marker write")
	}
}

// --- RecordApprovedOverride: forged markers must never record ---

func TestRecordApprovedOverrideHappyPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := newRepo(t)
	cfg := DefaultConfig()
	now := time.Now()
	cmd := "ls ../../sibling/dir"
	toolInput := []byte(`{"command":"` + cmd + `"}`)
	hash := InputHash(toolInput)

	WritePendingOverride(hash, PendingOverride{
		Pattern: "traversal", Shape: Shape(cmd), Root: repo, SessionID: "sess-1",
	}, now)

	pattern, recorded, err := RecordApprovedOverride(hash, "sess-1", cmd, repo, cfg, now)
	if err != nil || !recorded || pattern != "traversal" {
		t.Fatalf("want recorded traversal, got pattern=%q recorded=%v err=%v", pattern, recorded, err)
	}
	al, _ := Load(repo)
	if al == nil || al.Patterns["traversal"].Shapes[Shape(cmd)].Count != 1 {
		t.Fatal("override not persisted")
	}
}

func TestForgedMarkerForInnocuousCommandNotRecorded(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := newRepo(t)
	now := time.Now()
	// Attacker plants a marker claiming "traversal" for a command that
	// matches no guard pattern, then runs the innocuous command.
	cmd := "echo hi"
	toolInput := []byte(`{"command":"echo hi"}`)
	hash := InputHash(toolInput)
	WritePendingOverride(hash, PendingOverride{
		Pattern: "traversal", Shape: Shape(cmd), Root: repo, SessionID: "sess-1",
	}, now)

	_, recorded, err := RecordApprovedOverride(hash, "sess-1", cmd, repo, DefaultConfig(), now)
	if recorded {
		t.Fatal("forged marker must not record an override")
	}
	if err == nil {
		t.Error("forged marker should surface a validation error for audit")
	}
	if _, statErr := os.Stat(allowlistPath(repo)); !os.IsNotExist(statErr) {
		t.Error("no allowlist file should be created from a forged marker")
	}
}

func TestForgedMarkerShapeMismatchNotRecorded(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := newRepo(t)
	now := time.Now()
	// Marker shape is for a different (broader) command than what ran.
	cmd := "ls ../../sibling/dir"
	toolInput := []byte(`{"command":"` + cmd + `"}`)
	hash := InputHash(toolInput)
	WritePendingOverride(hash, PendingOverride{
		Pattern: "traversal", Shape: "rm -rf ../../everything", Root: repo, SessionID: "sess-1",
	}, now)

	if _, recorded, _ := RecordApprovedOverride(hash, "sess-1", cmd, repo, DefaultConfig(), now); recorded {
		t.Fatal("shape mismatch must not record")
	}
}

func TestForgedMarkerRootMismatchNotRecorded(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := newRepo(t)
	elsewhere := newRepo(t)
	now := time.Now()
	cmd := "ls ../../sibling/dir"
	toolInput := []byte(`{"command":"` + cmd + `"}`)
	hash := InputHash(toolInput)
	// Marker tries to plant the override into a different directory than
	// where the command actually ran.
	WritePendingOverride(hash, PendingOverride{
		Pattern: "traversal", Shape: Shape(cmd), Root: elsewhere, SessionID: "sess-1",
	}, now)

	if _, recorded, _ := RecordApprovedOverride(hash, "sess-1", cmd, repo, DefaultConfig(), now); recorded {
		t.Fatal("root mismatch must not record")
	}
	if _, err := os.Stat(allowlistPath(elsewhere)); !os.IsNotExist(err) {
		t.Error("no allowlist may be planted outside the repo the command ran in")
	}
}

func TestInputHashVariesWithInput(t *testing.T) {
	a := InputHash([]byte(`{"command":"a"}`))
	b := InputHash([]byte(`{"command":"b"}`))
	if a == b {
		t.Error("InputHash must vary with input")
	}
	if len(a) != 16 {
		t.Errorf("InputHash length = %d, want 16", len(a))
	}
}
