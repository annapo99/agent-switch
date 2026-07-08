package cli

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestNoArgsLaunchesTUI(t *testing.T) {
	originalRunTUI := runTUI
	var called bool
	runTUI = func(home string, stdin io.Reader, stdout, stderr io.Writer) int {
		called = true
		if home == "" {
			t.Fatal("home should be passed")
		}
		return 0
	}
	defer func() { runTUI = originalRunTUI }()
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Main(nil, strings.NewReader(""), &stdout, &stderr, t.TempDir())

	if code != 0 || !called {
		t.Fatalf("code=%d called=%v stdout=%q stderr=%q", code, called, stdout.String(), stderr.String())
	}
}

func TestHelpUsesAGSProgramName(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Main([]string{"--help"}, strings.NewReader(""), &stdout, &stderr, t.TempDir())

	if code != 0 {
		t.Fatalf("code = %d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "usage: ags") || !strings.Contains(stdout.String(), "save") {
		t.Fatalf("help:\n%s", stdout.String())
	}
}

func TestUnknownCommandReturnsError(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Main([]string{"wat"}, strings.NewReader(""), &stdout, &stderr, t.TempDir())

	if code != 2 || !strings.Contains(stderr.String(), "Unknown command: wat") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}
