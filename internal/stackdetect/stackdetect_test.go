package stackdetect

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEmptyDir(t *testing.T) {
	dir := t.TempDir()
	r := Detect(dir)
	if len(r.Stacks) != 0 {
		t.Fatalf("expected empty stacks, got %v", r.Stacks)
	}
}

func TestGoMod(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/foo\n\ngo 1.23\n")

	r := Detect(dir)
	assertStacks(t, r, []string{"go"})
	assertEqual(t, "TestCommand", r.TestCommand, "go test ./...")
	assertEqual(t, "LintCommand", r.LintCommand, "go vet ./...")
}

func TestPackageJSONWithTSConfig(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"name":"foo"}`)
	writeFile(t, dir, "tsconfig.json", `{}`)

	r := Detect(dir)
	assertStacks(t, r, []string{"typescript"})
	if !r.HasTSConfig {
		t.Error("expected HasTSConfig=true")
	}
}

func TestPackageJSONWithYarnLock(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"name":"foo"}`)
	writeFile(t, dir, "yarn.lock", "")

	r := Detect(dir)
	assertEqual(t, "PackageManager", r.PackageManager, "yarn")
}

func TestPackageJSONWithPnpmLock(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"name":"foo"}`)
	writeFile(t, dir, "pnpm-lock.yaml", "")

	r := Detect(dir)
	assertEqual(t, "PackageManager", r.PackageManager, "pnpm")
}

func TestPackageJSONWithBunLockb(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"name":"foo"}`)
	writeFile(t, dir, "bun.lockb", "")

	r := Detect(dir)
	assertEqual(t, "PackageManager", r.PackageManager, "bun")
}

func TestPackageJSONDefaultNPM(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"name":"foo"}`)

	r := Detect(dir)
	assertEqual(t, "PackageManager", r.PackageManager, "npm")
}

func TestPyprojectWithPoetryLock(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pyproject.toml", "[tool.poetry]\nname = \"foo\"\n")
	writeFile(t, dir, "poetry.lock", "")

	r := Detect(dir)
	assertStacks(t, r, []string{"python"})
	assertEqual(t, "PackageManager", r.PackageManager, "poetry")
	assertEqual(t, "TestCommand", r.TestCommand, "pytest")
	assertEqual(t, "LintCommand", r.LintCommand, "ruff check .")
	assertEqual(t, "FormatCommand", r.FormatCommand, "ruff format .")
}

func TestPyprojectWithUVLock(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pyproject.toml", "[project]\nname = \"foo\"\n")
	writeFile(t, dir, "uv.lock", "")

	r := Detect(dir)
	assertStacks(t, r, []string{"python"})
	assertEqual(t, "PackageManager", r.PackageManager, "uv")
}

func TestRequirementsTxt(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "requirements.txt", "flask\n")

	r := Detect(dir)
	assertStacks(t, r, []string{"python"})
	assertEqual(t, "PackageManager", r.PackageManager, "pip")
}

func TestCargoToml(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Cargo.toml", "[package]\nname = \"foo\"\n")

	r := Detect(dir)
	assertStacks(t, r, []string{"rust"})
	assertEqual(t, "TestCommand", r.TestCommand, "cargo test")
	assertEqual(t, "LintCommand", r.LintCommand, "cargo clippy")
	assertEqual(t, "FormatCommand", r.FormatCommand, "cargo fmt")
}

func TestGemfile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Gemfile", "source 'https://rubygems.org'\n")

	r := Detect(dir)
	assertStacks(t, r, []string{"ruby"})
}

func TestGoAndPackageJSON(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/foo\n\ngo 1.23\n")
	writeFile(t, dir, "package.json", `{"name":"foo"}`)

	r := Detect(dir)
	// JS detected first (package.json), then Go
	if len(r.Stacks) != 2 {
		t.Fatalf("expected 2 stacks, got %v", r.Stacks)
	}
	// Order: javascript first (package.json processed first), then go
	found := map[string]bool{}
	for _, s := range r.Stacks {
		found[s] = true
	}
	if !found["javascript"] && !found["typescript"] {
		t.Error("expected javascript or typescript in stacks")
	}
	if !found["go"] {
		t.Error("expected go in stacks")
	}
}

func TestPackageJSONWithTestScript(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"name":"foo","scripts":{"test":"jest"}}`)

	r := Detect(dir)
	assertEqual(t, "TestCommand", r.TestCommand, "npm test")
}

func TestPackageJSONWithLintScript(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"name":"foo","scripts":{"lint":"eslint ."}}`)

	r := Detect(dir)
	assertEqual(t, "LintCommand", r.LintCommand, "npm run lint")
}

func TestESLintDetection(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"name":"foo"}`)
	writeFile(t, dir, ".eslintrc.json", `{}`)

	r := Detect(dir)
	if !r.HasESLint {
		t.Error("expected HasESLint=true")
	}
	assertEqual(t, "LintCommand", r.LintCommand, "npx eslint .")
}

func TestESLintFlatConfig(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"name":"foo"}`)
	writeFile(t, dir, "eslint.config.js", "export default {};")

	r := Detect(dir)
	if !r.HasESLint {
		t.Error("expected HasESLint=true")
	}
}

func TestPrettierDetectionViaFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"name":"foo"}`)
	writeFile(t, dir, ".prettierrc", `{}`)

	r := Detect(dir)
	if !r.HasPrettier {
		t.Error("expected HasPrettier=true")
	}
	assertEqual(t, "FormatCommand", r.FormatCommand, "npx prettier --write .")
}

func TestPrettierDetectionViaDevDeps(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"name":"foo","devDependencies":{"prettier":"^3.0.0"}}`)

	r := Detect(dir)
	if !r.HasPrettier {
		t.Error("expected HasPrettier=true")
	}
}

func TestMonorepoLerna(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"name":"foo"}`)
	writeFile(t, dir, "lerna.json", `{}`)

	r := Detect(dir)
	if !r.Monorepo {
		t.Error("expected Monorepo=true")
	}
}

func TestMonorepoPnpmWorkspace(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"name":"foo"}`)
	writeFile(t, dir, "pnpm-workspace.yaml", "packages:\n  - packages/*\n")

	r := Detect(dir)
	if !r.Monorepo {
		t.Error("expected Monorepo=true")
	}
}

func TestMonorepoWorkspacesKey(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"name":"foo","workspaces":["packages/*"]}`)

	r := Detect(dir)
	if !r.Monorepo {
		t.Error("expected Monorepo=true")
	}
}

func TestMonorepoMultipleGoMods(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/root\n\ngo 1.23\n")
	sub := filepath.Join(dir, "submod")
	os.MkdirAll(sub, 0755)
	writeFile(t, sub, "go.mod", "module example.com/root/submod\n\ngo 1.23\n")

	r := Detect(dir)
	if !r.Monorepo {
		t.Error("expected Monorepo=true for multiple go.mod files")
	}
}

func TestStacksNeverNil(t *testing.T) {
	dir := t.TempDir()
	r := Detect(dir)
	if r.Stacks == nil {
		t.Error("expected non-nil Stacks slice")
	}
}

// --- helpers ---

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatalf("writeFile %s: %v", name, err)
	}
}

func assertStacks(t *testing.T, r *Result, want []string) {
	t.Helper()
	if len(r.Stacks) != len(want) {
		t.Fatalf("stacks: got %v, want %v", r.Stacks, want)
	}
	for i, s := range r.Stacks {
		if s != want[i] {
			t.Errorf("stacks[%d]: got %q, want %q", i, s, want[i])
		}
	}
}

func assertEqual(t *testing.T, field, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %q, want %q", field, got, want)
	}
}
