package main

import (
	"os"

	"github.com/annapo99/agent-switch/internal/cli"
)

var version = "0.1.0"

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	os.Exit(cli.Main(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, home))
}
