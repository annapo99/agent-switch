package main

import (
	"fmt"
	"os"
	"runtime/debug"

	"github.com/annapo99/agent-switch/internal/cli"
)

var version = "dev"

func main() {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		fmt.Fprintln(os.Stderr, "ags needs HOME to find agent logins")
		os.Exit(1)
	}
	os.Exit(cli.MainWithVersion(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, home, resolvedVersion()))
}

func resolvedVersion() string {
	if version != "dev" {
		return version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return version
	}
	return info.Main.Version
}
