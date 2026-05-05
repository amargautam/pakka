package specfind

import (
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed spec_match_prompt.md
var specMatchPromptTmpl string

// LLMCaller is mockable in tests.
type LLMCaller interface {
	Call(prompt string) (string, error)
}

// Options for Find.
type Options struct {
	SpecsDir string    // directory to scan; default "docs/specs/"
	Branch   string    // current git branch name
	Changed  []string  // changed file paths
	Override string    // --spec flag; if non-empty, skip discovery entirely
	LLM      LLMCaller // nil = use real claude -p via ClaudeSubprocess
}

// FindResult is the output of Find.
type FindResult struct {
	Path     string // empty if not found
	Advisory bool   // true if SpecsDir exists but no spec matched
}

// Find discovers the spec file. Discovery order:
// 1. Override non-empty → return it directly (no dir check)
// 2. SpecsDir absent → return empty FindResult (silent skip)
// 3. Name match: strip YYYY-MM-DD- prefix and .md suffix from each spec
//    filename, then do case-insensitive substring check against Branch and
//    each Changed path. First match by sorted filename wins (deterministic).
// 4. LLM fallback: build judge prompt from internal/specfind/spec_match_prompt.md,
//    call LLM.Call(prompt), parse JSON {"match":"...","confidence":0.0},
//    return path if confidence >= 0.7, else return Advisory=true.
// 5. No match → return FindResult{Advisory: true}
func Find(opts Options) (FindResult, error) {
	// Step 1: override
	if opts.Override != "" {
		return FindResult{Path: opts.Override}, nil
	}

	// Step 2: dir absent → silent skip
	specsDir := opts.SpecsDir
	if specsDir == "" {
		specsDir = "docs/specs/"
	}
	if _, err := os.Stat(specsDir); os.IsNotExist(err) {
		return FindResult{}, nil
	}

	// Read and sort spec files for deterministic matching.
	entries, err := os.ReadDir(specsDir)
	if err != nil {
		return FindResult{}, err
	}
	var specFiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			specFiles = append(specFiles, e.Name())
		}
	}
	sort.Strings(specFiles)

	// Step 3: name match — check if spec stem appears in branch or any changed path.
	branchLower := strings.ToLower(opts.Branch)
	changedLower := make([]string, len(opts.Changed))
	for i, c := range opts.Changed {
		changedLower[i] = strings.ToLower(c)
	}

	for _, name := range specFiles {
		stem := stemFromFilename(name)
		if stem == "" {
			continue
		}
		if strings.Contains(branchLower, stem) {
			return FindResult{Path: filepath.Join(specsDir, name)}, nil
		}
		for _, c := range changedLower {
			if strings.Contains(c, stem) {
				return FindResult{Path: filepath.Join(specsDir, name)}, nil
			}
		}
	}

	// Step 4: LLM fallback — defer to llmFind.
	return llmFind(opts, specsDir, specFiles)
}

// llmFind builds the judge prompt, calls the LLM, parses JSON, and returns
// the matched spec path or Advisory=true.
func llmFind(opts Options, specsDir string, specFiles []string) (FindResult, error) {
	llm := opts.LLM
	if llm == nil {
		llm = NewClaudeLLM()
	}

	if len(specFiles) == 0 {
		return FindResult{Advisory: true}, nil
	}

	prompt, err := buildJudgePrompt(opts, specsDir, specFiles)
	if err != nil {
		return FindResult{Advisory: true}, nil
	}

	resp, err := llm.Call(prompt)
	if err != nil {
		return FindResult{Advisory: true}, nil
	}

	match, confidence := parseMatchResponse(resp)
	if confidence >= 0.7 && match != "" {
		return FindResult{Path: filepath.Join(specsDir, match)}, nil
	}
	return FindResult{Advisory: true}, nil
}

// buildJudgePrompt constructs the LLM judge prompt by interpolating specs,
// branch, and changed files into the template embedded from
// spec_match_prompt.md.
func buildJudgePrompt(opts Options, specsDir string, specFiles []string) (string, error) {
	tmpl := specMatchPromptTmpl

	type specEntry struct {
		Filename           string `json:"filename"`
		Heading            string `json:"heading"`
		AcceptanceCriteria string `json:"acceptance_criteria"`
		OutOfScope         string `json:"out_of_scope"`
	}

	var entries []specEntry
	for _, name := range specFiles {
		sec, err := ParseSpec(filepath.Join(specsDir, name))
		if err != nil {
			continue
		}
		entries = append(entries, specEntry{
			Filename:           sec.Filename,
			Heading:            sec.Heading,
			AcceptanceCriteria: sec.AcceptanceCriteria,
			OutOfScope:         sec.OutOfScope,
		})
	}

	specsJSON, err := json.Marshal(entries)
	if err != nil {
		return "", err
	}

	changedStr := strings.Join(opts.Changed, ", ")
	prompt := strings.ReplaceAll(tmpl, "{{specs}}", string(specsJSON))
	prompt = strings.ReplaceAll(prompt, "{{branch}}", opts.Branch)
	prompt = strings.ReplaceAll(prompt, "{{changed_files}}", changedStr)
	return prompt, nil
}

// matchResponse is the JSON structure returned by the LLM judge.
type matchResponse struct {
	Match      string  `json:"match"`
	Confidence float64 `json:"confidence"`
}

// parseMatchResponse parses the LLM response JSON, stripping markdown fences
// if present. Returns ("", 0) on any parse failure.
func parseMatchResponse(resp string) (string, float64) {
	// Strip optional ```json ... ``` fences.
	s := strings.TrimSpace(resp)
	if strings.HasPrefix(s, "```") {
		lines := strings.SplitN(s, "\n", 2)
		if len(lines) == 2 {
			s = lines[1]
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
		s = strings.TrimSpace(s)
	}

	var mr matchResponse
	if err := json.Unmarshal([]byte(s), &mr); err != nil {
		return "", 0
	}
	return mr.Match, mr.Confidence
}
