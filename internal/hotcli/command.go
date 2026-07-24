package hotcli

// Command is the interface every hot-path subcommand implements. It mirrors
// internal/cli.Command so the guard/commit-gate/status-line conformance tests
// travel with the commands into this package.
type Command interface {
	Name() string
	Run(args []string) error
}
