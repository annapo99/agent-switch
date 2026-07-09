package app

import (
	"bytes"
	"encoding/json"
	"io"
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

func TestUpdateCommandRunsUpdater(t *testing.T) {
	originalRunUpdater := runUpdater
	defer func() { runUpdater = originalRunUpdater }()
	var gotHome string
	var gotVersion string
	runUpdater = func(home, version string, stdout, stderr io.Writer) int {
		gotHome = home
		gotVersion = version
		_, _ = io.WriteString(stdout, "Updated ags to v9.9.9\n")
		return 0
	}
	home := t.TempDir()
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := NewWithVersion(home, "v1.2.3").Run([]string{"update"}, strings.NewReader(""), &stdout, &stderr)

	if code != 0 || gotHome != home || gotVersion != "v1.2.3" ||
		!strings.Contains(stdout.String(), "Updated ags to v9.9.9") ||
		stderr.String() != "" {
		t.Fatalf("code=%d home=%q version=%q stdout=%q stderr=%q", code, gotHome, gotVersion, stdout.String(), stderr.String())
	}
}

func TestHelpIncludesUpdate(t *testing.T) {
	code, out, errOut := runService(t, t.TempDir(), []string{"--help"}, "")

	if code != 0 || errOut != "" || !strings.Contains(out, "update") {
		t.Fatalf("code=%d stderr=%q out=\n%s", code, errOut, out)
	}
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

func TestSaveSameLabelWithNewFingerprintUpdatesExistingProfile(t *testing.T) {
	home := t.TempDir()
	writeJSONFixture(t, home, ".claude/.credentials.json", map[string]any{"email": "annapo@example.com", "accessToken": "test-token-1"})
	if code, _, _ := runService(t, home, []string{"save", "--agent", "claude", "--yes"}, ""); code != 0 {
		t.Fatalf("first save code = %d", code)
	}
	firstManifest := readJSONFixture(t, home, ".agent-switch/profiles/claude/1/manifest.json")
	writeJSONFixture(t, home, ".claude/.credentials.json", map[string]any{"email": "annapo@example.com", "accessToken": "test-token-2"})

	code, out, errOut := runService(t, home, []string{"save", "--agent", "claude", "--yes"}, "")

	if code != 0 || errOut != "" {
		t.Fatalf("code=%d stderr=%q out=%q", code, errOut, out)
	}
	if !strings.Contains(out, "Updated claude account #1") || strings.Contains(out, "Saved claude account #2") {
		t.Fatalf("out:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(home, ".agent-switch/profiles/claude/2/manifest.json")); !os.IsNotExist(err) {
		t.Fatalf("duplicate manifest should not exist: %v", err)
	}
	nextManifest := readJSONFixture(t, home, ".agent-switch/profiles/claude/1/manifest.json")
	if nextManifest["fingerprint"] == firstManifest["fingerprint"] {
		t.Fatalf("fingerprint was not updated: %#v", nextManifest)
	}
	snapshot := readJSONFixture(t, home, ".agent-switch/profiles/claude/1/files/.claude/.credentials.json")
	if snapshot["accessToken"] != "test-token-2" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestSaveJSONListsDetectedCandidatesWithoutSaving(t *testing.T) {
	home := t.TempDir()
	writeJSONFixture(t, home, ".claude/.credentials.json", map[string]any{"email": "annapo@example.com", "accessToken": "test-token-1"})

	code, out, errOut := runService(t, home, []string{"save", "--json"}, "")

	if code != 0 || errOut != "" {
		t.Fatalf("code=%d stderr=%q out=%q", code, errOut, out)
	}
	var candidates []map[string]any
	if err := json.Unmarshal([]byte(out), &candidates); err != nil {
		t.Fatalf("json error: %v\n%s", err, out)
	}
	if len(candidates) != 1 ||
		candidates[0]["agent"] != "claude" ||
		candidates[0]["label"] != "annapo@example.com" ||
		candidates[0]["save_number"] != float64(1) {
		t.Fatalf("candidates = %#v", candidates)
	}
	if _, err := os.Stat(filepath.Join(home, ".agent-switch/profiles/claude/1/manifest.json")); !os.IsNotExist(err) {
		t.Fatalf("save --json should not create a profile: %v", err)
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
	if code != 0 || errOut != "" ||
		!strings.Contains(out, "Switched Claude to account #1. Running sessions may take up to ~30s to pick it up.") ||
		strings.Contains(out, "Switch account?") {
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

func TestListJSONMarksActiveProfile(t *testing.T) {
	home := t.TempDir()
	writeJSONFixture(t, home, ".claude/.credentials.json", map[string]any{"email": "first@example.com", "accessToken": "first-test-token"})
	if code, _, _ := runService(t, home, []string{"save", "--agent", "claude", "--yes"}, ""); code != 0 {
		t.Fatalf("save first code = %d", code)
	}
	writeJSONFixture(t, home, ".claude/.credentials.json", map[string]any{"email": "second@example.com", "accessToken": "second-test-token"})
	if code, _, _ := runService(t, home, []string{"save", "--agent", "claude", "--yes"}, ""); code != 0 {
		t.Fatalf("save second code = %d", code)
	}

	code, out, errOut := runService(t, home, []string{"list", "--json"}, "")

	if code != 0 || errOut != "" {
		t.Fatalf("code=%d stderr=%q out=%q", code, errOut, out)
	}
	var profiles []map[string]any
	if err := json.Unmarshal([]byte(out), &profiles); err != nil {
		t.Fatalf("json error: %v\n%s", err, out)
	}
	if len(profiles) != 2 || profiles[0]["active"] != nil || profiles[1]["active"] != true {
		t.Fatalf("profiles = %#v", profiles)
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
		!strings.Contains(out, "Switched Codex to account #1") ||
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
