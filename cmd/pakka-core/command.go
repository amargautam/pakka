package main

// Command is the interface every subcommand implements.
type Command interface {
	Name() string
	Run(args []string) error
}
