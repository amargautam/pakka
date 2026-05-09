// Package claudecli provides shared argv construction for claude -p subprocess calls.
// Centralises flag set so changes (model, tool policy, output format) propagate to
// all callers automatically.
package claudecli

// BuildArgs returns the base argv for `claude -p` invocations.
// model is optional — empty string omits the --model flag.
// The returned slice starts with "-p" and is safe to pass directly to exec.Command.
func BuildArgs(model string) []string {
	args := []string{
		"-p",
		"--output-format", "text",
		"--permission-mode", "default",
		"--allowedTools", "",
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	return args
}
