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
	runTUI = func(home string, stdin io.Reader, stdout, stderr io.Writer, version string) int {
		called = true
		if home == "" {
			t.Fatal("home should be passed")
		}
		if version != "dev" {
			t.Fatalf("version = %q", version)
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

func TestMainWithVersionPassesVersionToTUI(t *testing.T) {
	originalRunTUI := runTUI
	var gotVersion string
	runTUI = func(home string, stdin io.Reader, stdout, stderr io.Writer, version string) int {
		gotVersion = version
		return 0
	}
	defer func() { runTUI = originalRunTUI }()
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := MainWithVersion(nil, strings.NewReader(""), &stdout, &stderr, t.TempDir(), "v9.9.9")

	if code != 0 || gotVersion != "v9.9.9" {
		t.Fatalf("code=%d version=%q stdout=%q stderr=%q", code, gotVersion, stdout.String(), stderr.String())
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
