package specfind_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amargautam/pakka/internal/specfind"
)

// makeSpecsDir creates a temp dir containing the given spec filenames (empty files).
// Returns the dir path and a cleanup func.
func makeSpecsDir(t *testing.T, filenames ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range filenames {
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("create spec file: %v", err)
		}
		f.Close()
	}
	return dir
}

// fakeLLM is a test double that records whether it was called.
type fakeLLM struct {
	resp   string
	called bool
}

func (f *fakeLLM) Call(prompt string) (string, error) {
	f.called = true
	return f.resp, nil
}

func TestFind(t *testing.T) {
	const specFile = "2026-05-05-spec-anchored-review.md"

	tests := []struct {
		name          string
		opts          func(t *testing.T) specfind.Options
		wantPath      string // suffix match — use strings.HasSuffix; empty = expect empty path
		wantAdvisory  bool
		wantLLMCalled bool
	}{
		{
			name: "override bypasses all logic",
			opts: func(t *testing.T) specfind.Options {
				llm := &fakeLLM{}
				return specfind.Options{
					Override: "/some/spec.md",
					LLM:      llm,
				}
			},
			wantPath:      "/some/spec.md",
			wantAdvisory:  false,
			wantLLMCalled: false,
		},
		{
			name: "dir absent — silent skip, no error",
			opts: func(t *testing.T) specfind.Options {
				llm := &fakeLLM{}
				return specfind.Options{
					SpecsDir: "/tmp/nonexistent-pakka-specs-xyz",
					LLM:      llm,
				}
			},
			wantPath:      "",
			wantAdvisory:  false,
			wantLLMCalled: false,
		},
		{
			name: "name match via branch",
			opts: func(t *testing.T) specfind.Options {
				dir := makeSpecsDir(t, specFile)
				llm := &fakeLLM{}
				return specfind.Options{
					SpecsDir: dir,
					Branch:   "feat/spec-anchored-review",
					LLM:      llm,
				}
			},
			wantPath:      specFile,
			wantAdvisory:  false,
			wantLLMCalled: false,
		},
		{
			name: "name match via changed file path",
			opts: func(t *testing.T) specfind.Options {
				dir := makeSpecsDir(t, specFile)
				llm := &fakeLLM{}
				return specfind.Options{
					SpecsDir: dir,
					Branch:   "main",
					Changed:  []string{"docs/specs/spec-anchored-review.md"},
					LLM:      llm,
				}
			},
			wantPath:      specFile,
			wantAdvisory:  false,
			wantLLMCalled: false,
		},
		{
			name: "name match miss — LLM returns high confidence — spec path returned",
			opts: func(t *testing.T) specfind.Options {
				const undatedSpec = "spec-anchored-review.md"
				dir := makeSpecsDir(t, undatedSpec)
				llm := &fakeLLM{resp: `{"match":"spec-anchored-review.md","confidence":0.8}`}
				return specfind.Options{
					SpecsDir: dir,
					Branch:   "main",
					Changed:  []string{},
					LLM:      llm,
				}
			},
			wantPath:      "spec-anchored-review.md",
			wantAdvisory:  false,
			wantLLMCalled: true,
		},
		{
			name: "name match miss — LLM returns low confidence — Advisory=true, empty path",
			opts: func(t *testing.T) specfind.Options {
				const undatedSpec = "spec-anchored-review.md"
				dir := makeSpecsDir(t, undatedSpec)
				llm := &fakeLLM{resp: `{"match":"spec-anchored-review.md","confidence":0.5}`}
				return specfind.Options{
					SpecsDir: dir,
					Branch:   "main",
					Changed:  []string{},
					LLM:      llm,
				}
			},
			wantPath:      "",
			wantAdvisory:  true,
			wantLLMCalled: true,
		},
		{
			name: "all date-prefixed specs — name match miss — LLM not called, Advisory=true",
			opts: func(t *testing.T) specfind.Options {
				dir := makeSpecsDir(t,
					"2026-05-05-spec-anchored-review.md",
					"2026-05-07-spec-generation.md",
				)
				llm := &fakeLLM{resp: `{"match":"2026-05-07-spec-generation.md","confidence":0.9}`}
				return specfind.Options{
					SpecsDir: dir,
					Branch:   "main",
					Changed:  []string{},
					LLM:      llm,
				}
			},
			wantPath:      "",
			wantAdvisory:  true,
			wantLLMCalled: false,
		},
		{
			name: "mixed specs (some date-prefixed, some not) — name match miss — LLM called",
			opts: func(t *testing.T) specfind.Options {
				dir := makeSpecsDir(t,
					"2026-05-05-spec-anchored-review.md",
					"old-spec-no-date.md",
				)
				llm := &fakeLLM{resp: `{"match":"old-spec-no-date.md","confidence":0.85}`}
				return specfind.Options{
					SpecsDir: dir,
					Branch:   "main",
					Changed:  []string{},
					LLM:      llm,
				}
			},
			wantPath:      "old-spec-no-date.md",
			wantAdvisory:  false,
			wantLLMCalled: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := tc.opts(t)
			llm := opts.LLM.(*fakeLLM)

			result, err := specfind.Find(opts)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Path check: exact match when wantPath is absolute, suffix match otherwise.
			if tc.wantPath == "" {
				if result.Path != "" {
					t.Errorf("got Path=%q, want empty", result.Path)
				}
			} else if strings.HasPrefix(tc.wantPath, "/") {
				if result.Path != tc.wantPath {
					t.Errorf("got Path=%q, want %q", result.Path, tc.wantPath)
				}
			} else {
				if !strings.HasSuffix(result.Path, tc.wantPath) {
					t.Errorf("got Path=%q, want suffix %q", result.Path, tc.wantPath)
				}
			}

			if result.Advisory != tc.wantAdvisory {
				t.Errorf("Advisory=%v, want %v", result.Advisory, tc.wantAdvisory)
			}
			if llm.called != tc.wantLLMCalled {
				t.Errorf("LLM called=%v, want %v", llm.called, tc.wantLLMCalled)
			}
		})
	}
}
