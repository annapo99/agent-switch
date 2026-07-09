package cli

import (
	"io"

	"github.com/annapo99/agent-switch/internal/app"
	"github.com/annapo99/agent-switch/internal/tui"
)

var runTUI = tui.RunWithVersion

func Main(args []string, stdin io.Reader, stdout, stderr io.Writer, home string) int {
	return MainWithVersion(args, stdin, stdout, stderr, home, "dev")
}

func MainWithVersion(args []string, stdin io.Reader, stdout, stderr io.Writer, home, version string) int {
	if len(args) == 0 {
		return runTUI(home, stdin, stdout, stderr, version)
	}
	return app.NewWithVersion(home, version).Run(args, stdin, stdout, stderr)
}
