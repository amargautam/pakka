// Package stackdetect scans a directory for project markers and reports
// the detected language stacks, package managers, and tool commands.
//
// Exit codes (when invoked via pakka-core stack-detect): 0 always.
// Output: JSON Result on stdout.
package stackdetect

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Result holds the detected stack configuration for a project directory.
type Result struct {
	Stacks         []string `json:"stacks"`
	PackageManager string   `json:"package_manager,omitempty"`
	TestCommand    string   `json:"test_command,omitempty"`
	LintCommand    string   `json:"lint_command,omitempty"`
	FormatCommand  string   `json:"format_command,omitempty"`
	HasTSConfig    bool     `json:"has_tsconfig,omitempty"`
	HasESLint      bool     `json:"has_eslint,omitempty"`
	HasPrettier    bool     `json:"has_prettier,omitempty"`
	Monorepo       bool     `json:"monorepo,omitempty"`
}

// Detect scans dir for project markers and returns the detected configuration.
//
// Purpose: Identify language stacks, package managers, and standard tool
// commands by checking for well-known project files.
// Errors: Never returns an error; missing or unreadable files are silently skipped.
func Detect(dir string) *Result {
	r := &Result{}

	hasPackageJSON := fileExists(dir, "package.json")
	hasTSConfig := fileExists(dir, "tsconfig.json")
	hasGoMod := fileExists(dir, "go.mod")
	hasPyprojectToml := fileExists(dir, "pyproject.toml")
	hasRequirementsTxt := fileExists(dir, "requirements.txt")
	hasSetupPy := fileExists(dir, "setup.py")
	hasCargoToml := fileExists(dir, "Cargo.toml")
	hasGemfile := fileExists(dir, "Gemfile")

	// --- TypeScript / JavaScript ---
	if hasPackageJSON {
		if hasTSConfig {
			r.Stacks = append(r.Stacks, "typescript")
			r.HasTSConfig = true
		} else {
			r.Stacks = append(r.Stacks, "javascript")
		}

		// Package manager detection
		switch {
		case fileExists(dir, "yarn.lock"):
			r.PackageManager = "yarn"
		case fileExists(dir, "pnpm-lock.yaml"):
			r.PackageManager = "pnpm"
		case fileExists(dir, "bun.lockb"):
			r.PackageManager = "bun"
		default:
			r.PackageManager = "npm"
		}

		// Parse package.json for scripts and devDependencies
		detectJSTooling(dir, r)
	}

	// --- Go ---
	if hasGoMod {
		r.Stacks = append(r.Stacks, "go")
		if r.TestCommand == "" {
			r.TestCommand = "go test ./..."
		}
		if r.LintCommand == "" {
			r.LintCommand = "go vet ./..."
		}
	}

	// --- Python ---
	if hasPyprojectToml || hasRequirementsTxt || hasSetupPy {
		r.Stacks = append(r.Stacks, "python")

		// Package manager detection
		switch {
		case fileExists(dir, "poetry.lock"):
			r.PackageManager = "poetry"
		case fileExists(dir, "uv.lock"):
			r.PackageManager = "uv"
		default:
			if r.PackageManager == "" {
				r.PackageManager = "pip"
			}
		}

		if r.TestCommand == "" {
			r.TestCommand = "pytest"
		}
		if r.LintCommand == "" {
			r.LintCommand = "ruff check ."
		}
		if r.FormatCommand == "" {
			r.FormatCommand = "ruff format ."
		}
	}

	// --- Rust ---
	if hasCargoToml {
		r.Stacks = append(r.Stacks, "rust")
		if r.TestCommand == "" {
			r.TestCommand = "cargo test"
		}
		if r.LintCommand == "" {
			r.LintCommand = "cargo clippy"
		}
		if r.FormatCommand == "" {
			r.FormatCommand = "cargo fmt"
		}
	}

	// --- Ruby ---
	if hasGemfile {
		r.Stacks = append(r.Stacks, "ruby")
	}

	// --- Monorepo detection ---
	r.Monorepo = detectMonorepo(dir, hasPackageJSON)

	// Ensure stacks is never nil (marshal as [] not null)
	if r.Stacks == nil {
		r.Stacks = []string{}
	}

	return r
}

// detectJSTooling parses package.json for scripts, eslint, and prettier config.
func detectJSTooling(dir string, r *Result) {
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return
	}

	var pkg struct {
		Scripts struct {
			Test string `json:"test"`
			Lint string `json:"lint"`
		} `json:"scripts"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if json.Unmarshal(data, &pkg) != nil {
		return
	}

	pm := r.PackageManager

	if pkg.Scripts.Test != "" {
		r.TestCommand = pm + " test"
	}
	if pkg.Scripts.Lint != "" {
		r.LintCommand = pm + " run lint"
	}

	// ESLint detection
	if hasGlobMatch(dir, ".eslintrc") || hasGlobMatch(dir, "eslint.config") {
		r.HasESLint = true
		if r.LintCommand == "" {
			r.LintCommand = "npx eslint ."
		}
	}

	// Prettier detection
	if hasGlobMatch(dir, ".prettierrc") || pkg.DevDependencies["prettier"] != "" {
		r.HasPrettier = true
		if r.FormatCommand == "" {
			r.FormatCommand = "npx prettier --write ."
		}
	}
}

// detectMonorepo checks for common monorepo indicators.
func detectMonorepo(dir string, hasPackageJSON bool) bool {
	if fileExists(dir, "lerna.json") {
		return true
	}
	if fileExists(dir, "pnpm-workspace.yaml") {
		return true
	}

	// Check for workspaces key in package.json
	if hasPackageJSON {
		data, err := os.ReadFile(filepath.Join(dir, "package.json"))
		if err == nil {
			var pkg map[string]json.RawMessage
			if json.Unmarshal(data, &pkg) == nil {
				if _, ok := pkg["workspaces"]; ok {
					return true
				}
			}
		}
	}

	// Multiple go.mod files
	goModCount := 0
	entries, err := os.ReadDir(dir)
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			if fileExists(filepath.Join(dir, e.Name()), "go.mod") {
				goModCount++
				if goModCount >= 1 && fileExists(dir, "go.mod") {
					return true
				}
			}
		}
	}

	return false
}

// fileExists reports whether the named file exists in dir.
func fileExists(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

// hasGlobMatch checks whether any file in dir starts with the given prefix.
func hasGlobMatch(dir, prefix string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) {
			return true
		}
	}
	return false
}
