package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/annapo99/agent-switch/internal/app"
)

func TestInitialViewShowsMenu(t *testing.T) {
	model := newModel(func([]string) commandResult {
		t.Fatal("runner should not be called")
		return commandResult{}
	})

	view := model.View()

	if !strings.Contains(view, "Switch AI coding agent accounts") ||
		!strings.Contains(view, "Current") ||
		!strings.Contains(view, "Use") ||
		!strings.Contains(view, "Quit") {
		t.Fatalf("view:\n%s", view)
	}
}

func TestInitialViewShowsLogoAndColor(t *testing.T) {
	model := newModel(func([]string) commandResult {
		t.Fatal("runner should not be called")
		return commandResult{}
	})

	view := model.View()

	if !strings.Contains(view, "/ _ | ___ ____ ___") ||
		!strings.Contains(view, "Switch AI coding agent accounts") {
		t.Fatalf("view:\n%s", view)
	}
	if !strings.Contains(view, "\x1b[") {
		t.Fatalf("expected ANSI styles in view:\n%s", view)
	}
}

func TestInitialViewShowsRepositoryURL(t *testing.T) {
	model := newModel(func([]string) commandResult {
		t.Fatal("runner should not be called")
		return commandResult{}
	})

	view := model.View()

	if !strings.Contains(view, "https://github.com/annapo99/agent-switch") {
		t.Fatalf("view:\n%s", view)
	}
}

func TestServiceRunnerForcesColorForBufferedCommandOutput(t *testing.T) {
	home := t.TempDir()
	writeJSONFixture(t, home, ".claude/.credentials.json", map[string]any{
		"email":       "annapo@example.com",
		"accessToken": "test-token-1",
	})
	runner := newServiceRunner(app.New(home))

	save := runner([]string{"save", "--agent", "claude", "--yes"})
	if save.code != 0 {
		t.Fatalf("save code=%d output:\n%s", save.code, save.output)
	}
	result := runner([]string{"list"})

	if result.code != 0 {
		t.Fatalf("list code=%d output:\n%s", result.code, result.output)
	}
	if !strings.Contains(result.output, "\x1b[34mClaude") {
		t.Fatalf("expected colored Claude heading, got:\n%s", result.output)
	}
}

func TestOutputViewPreservesCommandOutputANSI(t *testing.T) {
	coloredOutput := "\x1b[1m\x1b[34mClaude\x1b[0m"
	model := newModel(func([]string) commandResult {
		t.Fatal("runner should not be called")
		return commandResult{}
	})
	model.screen = screenOutput
	model.title = "Current accounts"
	model.output = coloredOutput

	view := model.View()

	if strings.Contains(view, outputStyle.Render(coloredOutput)) {
		t.Fatalf("output should not be wrapped with another ANSI style:\n%q", view)
	}
	if !strings.Contains(view, coloredOutput) {
		t.Fatalf("expected command ANSI to be preserved:\n%q", view)
	}
}

func TestUpAndDownKeysMoveMenuCursor(t *testing.T) {
	model := newModel(func([]string) commandResult {
		t.Fatal("runner should not be called")
		return commandResult{}
	})

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated := next.(uiModel)
	if updated.cursor != 1 {
		t.Fatalf("down cursor = %d, want 1", updated.cursor)
	}

	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyUp})
	updated = next.(uiModel)
	if updated.cursor != 0 {
		t.Fatalf("up cursor = %d, want 0", updated.cursor)
	}
}

func TestRightKeyEntersCurrentSelection(t *testing.T) {
	var called bool
	model := newModel(func(args []string) commandResult {
		called = true
		if strings.Join(args, " ") != "current" {
			t.Fatalf("args = %q", strings.Join(args, " "))
		}
		return commandResult{code: 0, output: "Current accounts\n\nClaude\n"}
	})

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRight})
	loading := next.(uiModel)

	if called {
		t.Fatal("runner should not be called before returned command runs")
	}
	if cmd == nil || !strings.Contains(loading.View(), "Loading Current accounts") {
		t.Fatalf("loading view:\n%s", loading.View())
	}
}

func TestLeftKeyGoesBack(t *testing.T) {
	model := newModel(func([]string) commandResult {
		t.Fatal("runner should not be called")
		return commandResult{}
	})
	model.screen = screenOutput
	model.title = "Saved accounts"
	model.output = "Claude"

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyLeft})
	updated := next.(uiModel)

	if updated.screen != screenMenu {
		t.Fatalf("screen = %v, want menu", updated.screen)
	}
}

func TestCurrentMenuItemShowsLoadingBeforeCommandCompletes(t *testing.T) {
	var called bool
	model := newModel(func(args []string) commandResult {
		called = true
		if strings.Join(args, " ") != "current" {
			t.Fatalf("args = %q", strings.Join(args, " "))
		}
		return commandResult{code: 0, output: "Current accounts\n\nClaude\n"}
	})

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	loading := next.(uiModel)

	if called {
		t.Fatal("runner should not be called before returned command runs")
	}
	if cmd == nil || !strings.Contains(loading.View(), "Loading Current accounts") {
		t.Fatalf("loading view:\n%s", loading.View())
	}

	next, _ = loading.Update(runPrimaryCmd(t, cmd))
	updated := next.(uiModel)

	if !called || !strings.Contains(updated.View(), "Current accounts") {
		t.Fatalf("called=%v view:\n%s", called, updated.View())
	}
}

func TestCurrentMenuItemRunsCurrentCommand(t *testing.T) {
	var gotArgs []string
	model := newModel(func(args []string) commandResult {
		gotArgs = append([]string{}, args...)
		return commandResult{code: 0, output: "Current accounts\n\nClaude\n"}
	})

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	loading := next.(uiModel)
	next, _ = loading.Update(runPrimaryCmd(t, cmd))
	updated := next.(uiModel)

	if strings.Join(gotArgs, " ") != "current" {
		t.Fatalf("args = %q", strings.Join(gotArgs, " "))
	}
	if !strings.Contains(updated.View(), "Current accounts") ||
		!strings.Contains(updated.View(), "B Back") {
		t.Fatalf("view:\n%s", updated.View())
	}
}

func TestUseProfileRunsAgentScopedUseCommand(t *testing.T) {
	var calls [][]string
	model := newModel(func(args []string) commandResult {
		calls = append(calls, append([]string{}, args...))
		if strings.Join(args, " ") == "list --json" {
			return commandResult{code: 0, output: `[{"agent":"claude","display_name":"Claude","number":2,"label":"annapo@example.com"}]`}
		}
		return commandResult{code: 0, output: "Switched claude to account #2\n"}
	})
	model.cursor = menuIndexUse

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	loadingProfiles := next.(uiModel)
	next, _ = loadingProfiles.Update(runPrimaryCmd(t, cmd))
	profiles := next.(uiModel)
	next, cmd = profiles.Update(tea.KeyMsg{Type: tea.KeyEnter})
	loadingUse := next.(uiModel)
	next, _ = loadingUse.Update(runPrimaryCmd(t, cmd))
	updated := next.(uiModel)

	if len(calls) != 2 {
		t.Fatalf("calls = %#v", calls)
	}
	if got := strings.Join(calls[1], " "); got != "use 2 --agent claude --yes" {
		t.Fatalf("use args = %q", got)
	}
	if !strings.Contains(updated.View(), "Switched claude to account #2") {
		t.Fatalf("view:\n%s", updated.View())
	}
}

func TestRemoveProfileAsksForConfirmationBeforeRemoving(t *testing.T) {
	var calls [][]string
	model := newModel(func(args []string) commandResult {
		calls = append(calls, append([]string{}, args...))
		if strings.Join(args, " ") == "list --json" {
			return commandResult{code: 0, output: `[{"agent":"codex","display_name":"Codex","number":1,"label":"annapo.codex@example.com"}]`}
		}
		return commandResult{code: 0, output: "Removed codex account #1\n"}
	})
	model.cursor = menuIndexRemove

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	loadingProfiles := next.(uiModel)
	next, _ = loadingProfiles.Update(runPrimaryCmd(t, cmd))
	profiles := next.(uiModel)
	next, _ = profiles.Update(tea.KeyMsg{Type: tea.KeyEnter})
	confirm := next.(uiModel)

	if len(calls) != 1 {
		t.Fatalf("remove should not run before confirmation: %#v", calls)
	}
	if !strings.Contains(confirm.View(), "Remove codex #1?") ||
		!strings.Contains(confirm.View(), "Enter Confirm") {
		t.Fatalf("confirm view:\n%s", confirm.View())
	}

	next, cmd = confirm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	loadingRemove := next.(uiModel)
	next, _ = loadingRemove.Update(runPrimaryCmd(t, cmd))
	updated := next.(uiModel)

	if len(calls) != 2 {
		t.Fatalf("calls = %#v", calls)
	}
	if got := strings.Join(calls[1], " "); got != "remove 1 --agent codex --yes" {
		t.Fatalf("remove args = %q", got)
	}
	if !strings.Contains(updated.View(), "Removed codex account #1") {
		t.Fatalf("view:\n%s", updated.View())
	}
}

func runPrimaryCmd(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected command")
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return msg
	}
	for _, batched := range batch {
		msg := batched()
		switch msg.(type) {
		case commandFinishedMsg, profilesFinishedMsg:
			return msg
		}
	}
	t.Fatalf("batch did not contain a completion message: %#v", msg)
	return nil
}

func writeJSONFixture(t *testing.T, home, rel string, payload any) {
	t.Helper()
	path := filepath.Join(home, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
