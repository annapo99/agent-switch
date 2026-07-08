package cli

import (
	"io"

	"github.com/annapo99/agent-switch/internal/app"
	"github.com/annapo99/agent-switch/internal/tui"
)

var runTUI = tui.Run

func Main(args []string, stdin io.Reader, stdout, stderr io.Writer, home string) int {
	if len(args) == 0 {
		return runTUI(home, stdin, stdout, stderr)
	}
	return app.New(home).Run(args, stdin, stdout, stderr)
}
