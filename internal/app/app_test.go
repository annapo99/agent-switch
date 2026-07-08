package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/annapo99/agent-switch/internal/model"
)

type fakeUsage struct {
	metadata map[model.MetadataKey]model.Metadata
}

func (f fakeUsage) Load(home string) map[model.MetadataKey]model.Metadata {
	return f.metadata
}

func writeJSONFixture(t *testing.T, home, rel string, payload any) {
	t.Helper()
	path := filepath.Join(home, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(payload)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readJSONFixture(t *testing.T, home, rel string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func runService(t *testing.T, home string, args []string, input string) (int, string, string) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := New(home).Run(args, strings.NewReader(input), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestSaveSingleDetectedAccountAsksOnceAndSavesNextNumber(t *testing.T) {
	home := t.TempDir()
	writeJSONFixture(t, home, ".claude/.credentials.json", map[string]any{"email": "annapo@example.com", "accessToken": "test-token-1"})

	code, out, errOut := runService(t, home, []string{"save"}, "\n")

	if code != 0 || errOut != "" {
		t.Fatalf("code=%d stderr=%q out=%q", code, errOut, out)
	}
	if !strings.Contains(out, "Detected Claude account") ||
		!strings.Contains(out, "annapo@example.com") ||
		!strings.Contains(out, "└ save as #1") ||
		!strings.Contains(out, "Save this account? [Y/n]") ||
		!strings.Contains(out, "Saved claude account #1") {
		t.Fatalf("out:\n%s", out)
	}
	if strings.Count(out, "?") != 1 {
		t.Fatalf("question count in:\n%s", out)
	}
	manifest := readJSONFixture(t, home, ".agent-switch/profiles/claude/1/manifest.json")
	if manifest["agent"] != "claude" || manifest["label"] != "annapo@example.com" {
		t.Fatalf("manifest = %#v", manifest)
	}
}

func TestSaveMultipleDetectedAccountsEnterSavesAll(t *testing.T) {
	home := t.TempDir()
	writeJSONFixture(t, home, ".claude/.credentials.json", map[string]any{"email": "annapo@example.com", "accessToken": "test-token-1"})
	writeJSONFixture(t, home, ".codex/auth.json", map[string]any{"email": "annapo.codex@example.com", "access_token": "test-token-2"})

	code, out, errOut := runService(t, home, []string{"save"}, "\n")

	if code != 0 || errOut != "" {
		t.Fatalf("code=%d stderr=%q out=%q", code, errOut, out)
	}
	if !strings.Contains(out, "Which account should be saved? [1/2 Enter save all]") ||
		!strings.Contains(out, "Saved claude account #1") ||
		!strings.Contains(out, "Saved codex account #1") {
		t.Fatalf("out:\n%s", out)
	}
}

func TestSaveMultipleDetectedAccountsCanSaveSelectedOnly(t *testing.T) {
	home := t.TempDir()
	writeJSONFixture(t, home, ".claude/.credentials.json", map[string]any{"email": "annapo@example.com", "accessToken": "test-token-1"})
	writeJSONFixture(t, home, ".codex/auth.json", map[string]any{"email": "annapo.codex@example.com", "access_token": "test-token-2"})

	code, out, errOut := runService(t, home, []string{"save"}, "2\n")

	if code != 0 || errOut != "" {
		t.Fatalf("code=%d stderr=%q out=%q", code, errOut, out)
	}
	if strings.Contains(out, "Saved claude account #1") || !strings.Contains(out, "Saved codex account #1") {
		t.Fatalf("out:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(home, ".agent-switch/profiles/claude/1/manifest.json")); !os.IsNotExist(err) {
		t.Fatalf("claude manifest should not exist: %v", err)
	}
}

func TestSaveAlreadySavedAccountDoesNotPrompt(t *testing.T) {
	home := t.TempDir()
	writeJSONFixture(t, home, ".claude/.credentials.json", map[string]any{"email": "annapo@example.com", "accessToken": "test-token-1"})
	if code, _, _ := runService(t, home, []string{"save", "--yes"}, ""); code != 0 {
		t.Fatalf("save code = %d", code)
	}

	code, out, errOut := runService(t, home, []string{"save"}, "y\n")

	if code != 0 || errOut != "" {
		t.Fatalf("code=%d stderr=%q out=%q", code, errOut, out)
	}
	if !strings.Contains(out, "└ already saved as #1") || !strings.Contains(out, "Nothing to save.") {
		t.Fatalf("out:\n%s", out)
	}
	if strings.Contains(out, "Save this account?") {
		t.Fatalf("unexpected prompt:\n%s", out)
	}
}

func TestListCurrentUseAndRemove(t *testing.T) {
	home := t.TempDir()
	writeJSONFixture(t, home, ".claude/.credentials.json", map[string]any{"email": "first@example.com", "accessToken": "first-test-token"})
	if code, _, _ := runService(t, home, []string{"save", "--agent", "claude", "--yes"}, ""); code != 0 {
		t.Fatalf("save first code = %d", code)
	}
	writeJSONFixture(t, home, ".claude/.credentials.json", map[string]any{"email": "second@example.com", "accessToken": "second-test-token"})
	if code, _, _ := runService(t, home, []string{"save", "--agent", "claude", "--yes"}, ""); code != 0 {
		t.Fatalf("save second code = %d", code)
	}

	code, out, errOut := runService(t, home, []string{"list"}, "")
	if code != 0 || errOut != "" || !strings.Contains(out, "2: second@example.com (active)") {
		t.Fatalf("list code=%d err=%q out=\n%s", code, errOut, out)
	}

	code, out, errOut = runService(t, home, []string{"use", "1"}, "")
	if code != 0 || errOut != "" || !strings.Contains(out, "Switched claude to account #1") || strings.Contains(out, "Switch account?") {
		t.Fatalf("use code=%d err=%q out=\n%s", code, errOut, out)
	}
	active := readJSONFixture(t, home, ".claude/.credentials.json")
	if active["email"] != "first@example.com" {
		t.Fatalf("active = %#v", active)
	}

	code, out, errOut = runService(t, home, []string{"current"}, "")
	if code != 0 || errOut != "" || !strings.Contains(out, "1: first@example.com (active)") {
		t.Fatalf("current code=%d err=%q out=\n%s", code, errOut, out)
	}

	code, out, errOut = runService(t, home, []string{"remove", "1"}, "y\n")
	if code != 0 || errOut != "" || !strings.Contains(out, "Removed claude account #1") {
		t.Fatalf("remove code=%d err=%q out=\n%s", code, errOut, out)
	}
}

func TestUseAmbiguousNumberAsksOnceAndAppliesSelectedAgent(t *testing.T) {
	home := t.TempDir()
	writeJSONFixture(t, home, ".claude/.credentials.json", map[string]any{"email": "annapo@example.com", "accessToken": "claude-test-token"})
	if code, _, _ := runService(t, home, []string{"save", "--agent", "claude", "--yes"}, ""); code != 0 {
		t.Fatalf("save claude code = %d", code)
	}
	writeJSONFixture(t, home, ".codex/auth.json", map[string]any{"email": "annapo.codex@example.com", "access_token": "codex-test-token"})
	if code, _, _ := runService(t, home, []string{"save", "--agent", "codex", "--yes"}, ""); code != 0 {
		t.Fatalf("save codex code = %d", code)
	}
	writeJSONFixture(t, home, ".codex/auth.json", map[string]any{"email": "annapo.other@example.com", "access_token": "other-test-token"})

	code, out, errOut := runService(t, home, []string{"use", "1"}, "2\n")

	if code != 0 || errOut != "" {
		t.Fatalf("code=%d stderr=%q out=%q", code, errOut, out)
	}
	if !strings.Contains(out, "Multiple accounts match #1") ||
		!strings.Contains(out, "2  codex   annapo.codex@example.com") ||
		!strings.Contains(out, "Switched codex to account #1") ||
		strings.Count(out, "?") != 1 {
		t.Fatalf("out:\n%s", out)
	}
	active := readJSONFixture(t, home, ".codex/auth.json")
	if active["email"] != "annapo.codex@example.com" || active["access_token"] != "codex-test-token" {
		t.Fatalf("active codex = %#v", active)
	}
}

func TestCurrentAddsUsageMetadata(t *testing.T) {
	home := t.TempDir()
	writeJSONFixture(t, home, ".claude/.credentials.json", map[string]any{"email": "annapo.claude@example.com", "accessToken": "test-token-1"})
	if code, _, _ := runService(t, home, []string{"save", "--agent", "claude", "--yes"}, ""); code != 0 {
		t.Fatalf("save code = %d", code)
	}
	service := NewWithUsageProvider(home, fakeUsage{metadata: map[model.MetadataKey]model.Metadata{
		{Agent: "claude", Label: "annapo.claude@example.com"}: {
			"organization_name": "Example Team",
			"usage_limits": []any{
				map[string]any{"label": "5h", "used_percentage": float64(90), "reset_at": "11:29", "remaining": "in 1m"},
			},
			"oauth_status": "oauth: fresh",
		},
	}})
	var out bytes.Buffer

	code := service.Current(&out, "")

	if code != 0 || !strings.Contains(out.String(), "1: annapo.claude@example.com [Example Team] (active)") ||
		!strings.Contains(out.String(), "└ • oauth: fresh") {
		t.Fatalf("out:\n%s", out.String())
	}
}

func TestListUsesColorWhenColorOutputIsEnabled(t *testing.T) {
	home := t.TempDir()
	writeJSONFixture(t, home, ".claude/.credentials.json", map[string]any{"email": "annapo@example.com", "accessToken": "test-token-1"})
	if code, _, _ := runService(t, home, []string{"save", "--agent", "claude", "--yes"}, ""); code != 0 {
		t.Fatalf("save code = %d", code)
	}
	originalShouldColor := shouldColor
	shouldColor = func(any) bool { return true }
	defer func() { shouldColor = originalShouldColor }()
	service := NewWithUsageProvider(home, fakeUsage{})
	var out bytes.Buffer

	code := service.List(&out, "", false)

	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if !strings.Contains(out.String(), "\033[34mClaude") {
		t.Fatalf("expected colored Claude heading, got:\n%s", out.String())
	}
}
