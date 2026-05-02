package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	evalPkg "github.com/amargautam/pakka/internal/eval"
)

// EvalCmd implements the "eval" subcommand.
type EvalCmd struct{}

func (c *EvalCmd) Name() string { return "eval" }
func (c *EvalCmd) Run(args []string) error {
	runEval()
	return nil
}

// --- eval (Pass 4) ---

func runEval() {
	maxLayer := 3 // default: all layers
	var targets []string
	for _, a := range os.Args[2:] {
		if strings.HasPrefix(a, "--layer=") {
			v := strings.TrimPrefix(a, "--layer=")
			n, err := strconv.Atoi(v)
			if err == nil {
				maxLayer = n
			}
		} else {
			targets = append(targets, a)
		}
	}

	// Auto-discover targets if none provided.
	if len(targets) == 0 {
		root := pluginRoot()
		targets = discoverEvalTargets(root)
	}

	if len(targets) == 0 {
		fmt.Fprintf(os.Stderr, "pakka: eval: no target files found\n")
		os.Exit(2)
	}

	result := evalPkg.Run(targets, maxLayer)

	// Print layer results as JSON lines to stderr.
	for _, lr := range result.Layers {
		data, _ := json.Marshal(lr)
		fmt.Fprintln(os.Stderr, string(data))
	}

	// Write full results to .pakka/eval/<ts>.json.
	ts := time.Now().Format("20060102-150405")
	evalDir := ".pakka/eval"
	_ = os.MkdirAll(evalDir, 0755)
	data, _ := json.MarshalIndent(result, "", "  ")
	_ = os.WriteFile(filepath.Join(evalDir, ts+".json"), data, 0644)

	if !result.Passed {
		os.Exit(2)
	}
}

// discoverEvalTargets finds skill, agent, and command files under the
// plugin root.
//
// Purpose: Auto-discover targets when none are provided on the command line.
// The eval package classifies each path and applies the right schema.
// Errors: Silently skips unreadable directories; returns nil if none found.
func discoverEvalTargets(root string) []string {
	var targets []string

	// skills/*/SKILL.md
	skillsDir := filepath.Join(root, "skills")
	if entries, err := os.ReadDir(skillsDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			p := filepath.Join(skillsDir, e.Name(), "SKILL.md")
			if _, err := os.Stat(p); err == nil {
				targets = append(targets, p)
			}
		}
	}

	// agents/*.md
	agentsDir := filepath.Join(root, "agents")
	if entries, err := os.ReadDir(agentsDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if strings.HasSuffix(e.Name(), ".md") {
				targets = append(targets, filepath.Join(agentsDir, e.Name()))
			}
		}
	}

	// commands/*.md
	commandsDir := filepath.Join(root, "commands")
	if entries, err := os.ReadDir(commandsDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if strings.HasSuffix(e.Name(), ".md") {
				targets = append(targets, filepath.Join(commandsDir, e.Name()))
			}
		}
	}

	return targets
}
