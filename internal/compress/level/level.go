// Package level is the single source of truth for pakka's output-compression
// level enum and its defaulting policy (issue #28).
//
// It is deliberately dependency-free (stdlib only, and in fact no imports at
// all) so that the hot-path binary (cmd/pakka-hot, via internal/hotcli) can
// resolve a level WITHOUT linking internal/compress/semantic — which pulls in
// net/http (the Anthropic client) and adds ~2ms of process-startup cost that
// the PreToolUse/statusLine hooks must not pay. semantic.Level / ParseLevel /
// AllLevels are type/behaviour aliases over this package, so every caller
// converges on one implementation.
package level

// Level controls prompt template aggressiveness / output-compression intensity.
type Level string

const (
	Lite       Level = "lite"
	Strict     Level = "strict"
	Ultra      Level = "ultra"
	SuperUltra Level = "super-ultra"
)

// All lists every supported level in increasing aggressiveness.
func All() []Level {
	return []Level{Lite, Strict, Ultra, SuperUltra}
}

// Parse converts a string to a Level, defaulting to SuperUltra.
//
// SuperUltra is pakka's brand default — decided v0.2.0 (2026-05-02); the older
// "ultra (2026-04-29)" default is SUPERSEDED. Empty/unknown inputs fall back to
// super-ultra so every fallback in the codebase converges on one tier (#28).
//
// Never errors; unknown strings map to SuperUltra (intentional default).
func Parse(s string) Level {
	switch Level(s) {
	case Lite, Strict, Ultra, SuperUltra:
		return Level(s)
	default:
		return SuperUltra
	}
}
