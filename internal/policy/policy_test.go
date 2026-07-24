package policy

import (
	"os"
	"path/filepath"
	"testing"
)

// writePolicy writes a policy.json into <repo>/.pakka/ and returns the repo root.
func writePolicy(t *testing.T, body string) string {
	t.Helper()
	repo := t.TempDir()
	dir := filepath.Join(repo, ".pakka")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "policy.json"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	return repo
}

// --- absence / no-op behavior (AC1) ---

func TestLoadAbsentFileIsNoPolicy(t *testing.T) {
	repo := t.TempDir() // no .pakka/policy.json
	p, err := Load(repo)
	if err != nil {
		t.Fatalf("absent policy must not error, got %v", err)
	}
	if p.Present() {
		t.Fatal("absent policy must not be Present()")
	}
	// Every enforcement helper is a no-op.
	if eff, cl := p.ClampConfidenceThreshold(95); eff != 95 || cl {
		t.Fatalf("no-policy threshold clamp = (%d,%v), want (95,false)", eff, cl)
	}
	if eff, cl := p.ClampMarkerFreshness(1800); eff != 1800 || cl {
		t.Fatalf("no-policy freshness clamp = (%d,%v), want (1800,false)", eff, cl)
	}
	if p.IsCategoryLocked("eval") {
		t.Fatal("no-policy must not lock any category")
	}
	if p.InputCompressLockedOff() {
		t.Fatal("no-policy must not lock input compress off")
	}
}

func TestLoadEmptyRepoRoot(t *testing.T) {
	p, err := Load("")
	if err != nil || p.Present() {
		t.Fatalf("empty root = (%v, present=%v), want (nil, false)", err, p.Present())
	}
}

// --- confidence threshold clamp (AC2) ---

func TestConfidenceThresholdClampsDownOnly(t *testing.T) {
	repo := writePolicy(t, `{"v":1,"confidenceThreshold":80}`)
	p, err := Load(repo)
	if err != nil {
		t.Fatal(err)
	}
	// Local above policy → clamped down to policy.
	if eff, cl := p.ClampConfidenceThreshold(95); eff != 80 || !cl {
		t.Fatalf("local 95 clamp = (%d,%v), want (80,true)", eff, cl)
	}
	// Local stricter (lower) → unchanged, no clamp.
	if eff, cl := p.ClampConfidenceThreshold(70); eff != 70 || cl {
		t.Fatalf("local 70 clamp = (%d,%v), want (70,false)", eff, cl)
	}
	// Local equal → no clamp.
	if eff, cl := p.ClampConfidenceThreshold(80); eff != 80 || cl {
		t.Fatalf("local 80 clamp = (%d,%v), want (80,false)", eff, cl)
	}
}

// --- marker freshness clamp + range (AC6) ---

func TestMarkerFreshnessClampAndDefault(t *testing.T) {
	repo := writePolicy(t, `{"v":1,"markerFreshnessSeconds":300}`)
	p, err := Load(repo)
	if err != nil {
		t.Fatal(err)
	}
	// Local 3600 with policy 300 → 300 wins.
	if eff, cl := p.ClampMarkerFreshness(3600); eff != 300 || !cl {
		t.Fatalf("clamp = (%d,%v), want (300,true)", eff, cl)
	}
	// Local below policy → unchanged.
	if eff, cl := p.ClampMarkerFreshness(120); eff != 120 || cl {
		t.Fatalf("clamp = (%d,%v), want (120,false)", eff, cl)
	}
}

func TestMarkerFreshnessAboveCeilingRejected(t *testing.T) {
	repo := writePolicy(t, `{"v":1,"markerFreshnessSeconds":7200}`)
	_, err := Load(repo)
	if err == nil {
		t.Fatal("markerFreshnessSeconds above ceiling must be rejected")
	}
	pe, ok := err.(*PolicyError)
	if !ok {
		t.Fatalf("want *PolicyError, got %T", err)
	}
	if !contains(pe.Msg, "markerFreshnessSeconds") {
		t.Fatalf("error must name the field, got %q", pe.Msg)
	}
}

func TestMarkerFreshnessAtCeilingAccepted(t *testing.T) {
	repo := writePolicy(t, `{"v":1,"markerFreshnessSeconds":3600}`)
	p, err := Load(repo)
	if err != nil {
		t.Fatalf("value at ceiling must be accepted, got %v", err)
	}
	if p.MarkerFreshnessSeconds != 3600 {
		t.Fatalf("MarkerFreshnessSeconds = %d, want 3600", p.MarkerFreshnessSeconds)
	}
}

// --- locked categories (AC3) ---

func TestLockedCategories(t *testing.T) {
	repo := writePolicy(t, `{"v":1,"lockedCategories":["secrets","eval"]}`)
	p, err := Load(repo)
	if err != nil {
		t.Fatal(err)
	}
	if !p.IsCategoryLocked("eval") {
		t.Fatal("eval must be locked")
	}
	if !p.IsCategoryLocked("secrets") {
		t.Fatal("secrets must be locked")
	}
	if p.IsCategoryLocked("traversal") {
		t.Fatal("traversal must not be locked")
	}
}

func TestLockedCategoriesUnknownEntryFailsClosed(t *testing.T) {
	repo := writePolicy(t, `{"v":1,"lockedCategories":["traversal","evel"]}`)
	_, err := Load(repo)
	pe, ok := err.(*PolicyError)
	if !ok {
		t.Fatalf("unknown lockedCategories entry must produce *PolicyError, got %T", err)
	}
	if !contains(pe.Msg, `"evel"`) {
		t.Fatalf("error must name the bad entry, got %q", pe.Msg)
	}
}

func TestLockedCategoriesAllKnownAccepted(t *testing.T) {
	repo := writePolicy(t, `{"v":1,"lockedCategories":["eval","shell-c-eval","pipe-shell","download-exec","traversal","system-path","secrets"]}`)
	if _, err := Load(repo); err != nil {
		t.Fatalf("all known categories must load, got %v", err)
	}
}

// --- input compress lock (AC4) ---

func TestInputCompressLockedOff(t *testing.T) {
	repo := writePolicy(t, `{"v":1,"inputCompress":"locked-off"}`)
	p, err := Load(repo)
	if err != nil {
		t.Fatal(err)
	}
	if !p.InputCompressLockedOff() {
		t.Fatal("inputCompress locked-off must report true")
	}

	repo2 := writePolicy(t, `{"v":1,"inputCompress":"opt-in"}`)
	p2, _ := Load(repo2)
	if p2.InputCompressLockedOff() {
		t.Fatal("non-locked-off value must report false")
	}
}

// --- version + malformed fail closed (AC7) ---

func TestNewerVersionFailsClosed(t *testing.T) {
	repo := writePolicy(t, `{"v":2}`)
	_, err := Load(repo)
	pe, ok := err.(*PolicyError)
	if !ok {
		t.Fatalf("v:2 must produce *PolicyError, got %T (%v)", err, err)
	}
	if !contains(pe.Msg, "version") {
		t.Fatalf("error must mention version, got %q", pe.Msg)
	}
}

func TestMalformedJSONFailsClosed(t *testing.T) {
	repo := writePolicy(t, `{"v":1,`)
	_, err := Load(repo)
	pe, ok := err.(*PolicyError)
	if !ok {
		t.Fatalf("malformed JSON must produce *PolicyError, got %T", err)
	}
	// Message names the file path (AC7).
	if !contains(pe.Error(), "policy.json") {
		t.Fatalf("error must name the file, got %q", pe.Error())
	}
}

// --- unknown keys warn but succeed ---

func TestUnknownKeysWarnAndContinue(t *testing.T) {
	repo := writePolicy(t, `{"v":1,"confidenceThreshold":80,"futureKnob":true}`)
	p, err := Load(repo)
	if err != nil {
		t.Fatalf("unknown key must not fail load, got %v", err)
	}
	if len(p.Warnings) == 0 {
		t.Fatal("unknown key must produce a warning")
	}
	if p.ConfidenceThreshold != 80 {
		t.Fatalf("known keys must still parse alongside unknown, got %d", p.ConfidenceThreshold)
	}
}

// --- version omitted defaults to acceptable ---

func TestVersionOmittedAccepted(t *testing.T) {
	repo := writePolicy(t, `{"confidenceThreshold":80}`)
	p, err := Load(repo)
	if err != nil {
		t.Fatalf("policy without v must load, got %v", err)
	}
	if !p.Present() {
		t.Fatal("policy must be Present()")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
