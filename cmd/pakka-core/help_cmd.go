package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// HelpCmd implements the "help" subcommand.
type HelpCmd struct{}

func (c *HelpCmd) Name() string { return "help" }
func (c *HelpCmd) Run(args []string) error {
	runHelp()
	return nil
}

// --- help (Pass 3.1) ---

func runHelp() {
	root := pluginRoot()

	data, _ := os.ReadFile(filepath.Join(root, "settings.json"))
	var s settingsJSON
	_ = json.Unmarshal(data, &s)

	// Resolve config with defaults
	autoGate := true
	threshold := 80
	guardOn := true
	sigOn := true
	coAuthorOn := true

	if s.Pakka.Review.AutoGate != nil {
		autoGate = *s.Pakka.Review.AutoGate
	}
	if s.Pakka.Review.ConfidenceThreshold != nil {
		threshold = *s.Pakka.Review.ConfidenceThreshold
	}
	if s.Pakka.Signature != nil {
		sigOn = *s.Pakka.Signature
	}
	if s.Pakka.CoAuthor != nil {
		coAuthorOn = *s.Pakka.CoAuthor
	}
	_ = guardOn // guard is always on if the hook is registered

	// Find latest session from audit files
	home, _ := os.UserHomeDir()
	auditDir := filepath.Join(home, ".pakka", "audit")
	meterDir := filepath.Join(home, ".pakka", "meter")

	sessionID := "none"
	auditFile := ""
	auditCount := 0
	meterFile := ""
	meterTok := 0

	if entries, err := os.ReadDir(auditDir); err == nil {
		var latestName string
		var latestTime time.Time
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			if info.ModTime().After(latestTime) {
				latestTime = info.ModTime()
				latestName = e.Name()
			}
		}
		if latestName != "" {
			sessionID = strings.TrimSuffix(latestName, ".jsonl")
			auditFile = "~/.pakka/audit/" + latestName
			auditCount = countJSONLEvents(filepath.Join(auditDir, latestName))
		}
	}

	meterPath := filepath.Join(meterDir, sessionID+".jsonl")
	if _, err := os.Stat(meterPath); err == nil {
		meterFile = "~/.pakka/meter/" + sessionID + ".jsonl"
		meterTok = countTokens(meterPath)
	}

	onOff := func(b bool) string {
		if b {
			return "on"
		}
		return "off"
	}

	outputLevel := loadOutputLevel()
	outputOn := isOutputEnabled()
	inputOn := isInputEnabled()

	fmt.Printf("pakka v%s · session %s\n", version, sessionID)
	fmt.Printf("  auto         review-gate: %-3s (threshold %d)  · input-compress: %s\n", onOff(autoGate), threshold, onOff(inputOn))
	fmt.Printf("               guard: %-3s                       · signature: %s\n", onOff(guardOn), onOff(sigOn))
	fmt.Printf("               coAuthor: %-3s                    · output: %s [%s]\n", onOff(coAuthorOn), onOff(outputOn), outputLevel)
	fmt.Printf("  commands     /pakka:review    explicit review of staged diff\n")
	fmt.Printf("               /pakka:compress  switch output level (lite|strict|ultra|super-ultra)\n")
	fmt.Printf("               /pakka:help      this page\n")
	if auditFile != "" {
		fmt.Printf("  audit        %s  · %d events\n", auditFile, auditCount)
	} else {
		fmt.Printf("  audit        (no session)\n")
	}
	if meterFile != "" {
		fmt.Printf("  meter        %s  · %d tok\n", meterFile, meterTok)
	} else {
		fmt.Printf("  meter        (no session)\n")
	}
	fmt.Printf("  attribution  pakka <279024857+pakka-bot@users.noreply.github.com>\n")
	fmt.Printf("  docs         pakka.dev\n")
}

func countJSONLEvents(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	count := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	// Subtract the schema preamble line
	if count > 0 {
		count--
	}
	return count
}

func countTokens(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	total := 0
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry struct {
			TokensUsed int `json:"tokens_used"`
		}
		if json.Unmarshal([]byte(line), &entry) == nil {
			total += entry.TokensUsed
		}
	}
	return total
}
