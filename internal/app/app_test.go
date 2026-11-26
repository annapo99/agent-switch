package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/annapo99/agent-switch/internal/claudebg"
	"github.com/annapo99/agent-switch/internal/model"
	"github.com/annapo99/agent-switch/internal/provider"
)

type fakeUsage struct {
	metadata map[model.MetadataKey]model.Metadata
}

func (f fakeUsage) Load(home string) map[model.MetadataKey]model.Metadata {
	return f.metadata
}

type profileSnapshotUsage struct{}

func (profileSnapshotUsage) Load(home string) map[model.MetadataKey]model.Metadata {
	snapshot := readJSONFromPath(filepath.Join(home, ".agent-switch/profiles/claude/1/files/.claude/.credentials.json"))
	status := "oauth: expired"
	if snapshot["accessToken"] == "refreshed-token" {
		status = "oauth: fresh"
	}
	return map[model.MetadataKey]model.Metadata{
		{Agent: "claude", Label: "annapo@example.com"}: {"oauth_status": status},
	}
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

func readJSONFromPath(path string) map[string]any {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return map[string]any{}
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

func TestSaveSeparatesClaudeOrganizationsAndRotatesOnlyMatchingProfile(t *testing.T) {
	home := t.TempDir()
	writeJSONFixture(t, home, ".claude/.credentials.json", map[string]any{
		"email": "same@example.com", "accessToken": "team-token",
	})
	writeJSONFixture(t, home, ".claude.json", map[string]any{
		"oauthAccount": map[string]any{
			"emailAddress": "same@example.com", "accountUuid": "account-1",
			"organizationUuid": "team-org", "organizationName": "Example Team",
		},
	})
	if code, _, _ := runService(t, home, []string{"save", "--agent", "claude", "--yes"}, ""); code != 0 {
		t.Fatalf("team save code = %d", code)
	}

	writeJSONFixture(t, home, ".claude/.credentials.json", map[string]any{
		"email": "same@example.com", "accessToken": "enterprise-token",
	})
	writeJSONFixture(t, home, ".claude.json", map[string]any{
		"oauthAccount": map[string]any{
			"emailAddress": "same@example.com", "accountUuid": "account-1",
			"organizationUuid": "enterprise-org", "organizationName": "Example Enterprise",
		},
	})
	code, out, errOut := runService(t, home, []string{"save", "--agent", "claude", "--yes"}, "")
	if code != 0 || errOut != "" || !strings.Contains(out, "Saved claude account #2") {
		t.Fatalf("enterprise save code=%d stderr=%q out=%q", code, errOut, out)
	}
	enterpriseBefore := readJSONFixture(t, home, ".agent-switch/profiles/claude/2/manifest.json")

	writeJSONFixture(t, home, ".claude/.credentials.json", map[string]any{
		"email": "same@example.com", "accessToken": "team-rotated-token",
	})
	writeJSONFixture(t, home, ".claude.json", map[string]any{
		"oauthAccount": map[string]any{
			"emailAddress": "same@example.com", "accountUuid": "account-1",
			"organizationUuid": "team-org", "organizationName": "Example Team",
		},
	})
	code, out, errOut = runService(t, home, []string{"save", "--agent", "claude", "--yes"}, "")
	if code != 0 || errOut != "" || !strings.Contains(out, "Updated claude account #1") {
		t.Fatalf("team rotation code=%d stderr=%q out=%q", code, errOut, out)
	}

	team := readJSONFixture(t, home, ".agent-switch/profiles/claude/1/manifest.json")
	enterprise := readJSONFixture(t, home, ".agent-switch/profiles/claude/2/manifest.json")
	teamMetadata := team["metadata"].(map[string]any)
	enterpriseMetadata := enterprise["metadata"].(map[string]any)
	if teamMetadata[model.MetadataOrganizationUUID] != "team-org" ||
		enterpriseMetadata[model.MetadataOrganizationUUID] != "enterprise-org" {
		t.Fatalf("team=%#v enterprise=%#v", teamMetadata, enterpriseMetadata)
	}
	if enterprise["fingerprint"] != enterpriseBefore["fingerprint"] {
		t.Fatalf("enterprise changed: before=%#v after=%#v", enterpriseBefore, enterprise)
	}
	if _, err := os.Stat(filepath.Join(home, ".agent-switch/profiles/claude/3/manifest.json")); !os.IsNotExist(err) {
		t.Fatalf("unexpected third profile: %v", err)
	}
}

func TestListSyncsOnlyTheActiveClaudeOrganization(t *testing.T) {
	home := t.TempDir()
	saveOrganization := func(token, orgUUID, orgName string) {
		t.Helper()
		writeJSONFixture(t, home, ".claude/.credentials.json", map[string]any{
			"email": "same@example.com", "accessToken": token,
		})
		writeJSONFixture(t, home, ".claude.json", map[string]any{
			"oauthAccount": map[string]any{
				"emailAddress": "same@example.com", "accountUuid": "account-1",
				"organizationUuid": orgUUID, "organizationName": orgName,
			},
		})
		if code, _, _ := runService(t, home, []string{"save", "--agent", "claude", "--yes"}, ""); code != 0 {
			t.Fatalf("save %s code = %d", orgUUID, code)
		}
	}
	saveOrganization("team-token", "team-org", "Example Team")
	saveOrganization("enterprise-token", "enterprise-org", "Example Enterprise")
	teamBefore := readJSONFixture(t, home, ".agent-switch/profiles/claude/1/manifest.json")
	enterpriseBefore := readJSONFixture(t, home, ".agent-switch/profiles/claude/2/manifest.json")

	writeJSONFixture(t, home, ".claude/.credentials.json", map[string]any{
		"email": "same@example.com", "accessToken": "enterprise-rotated-token",
	})
	writeJSONFixture(t, home, ".claude.json", map[string]any{
		"oauthAccount": map[string]any{
			"emailAddress": "same@example.com", "accountUuid": "account-1",
			"organizationUuid": "enterprise-org", "organizationName": "Example Enterprise",
		},
	})

	code, _, errOut := runService(t, home, []string{"list", "--agent", "claude"}, "")
	if code != 0 || errOut != "" {
		t.Fatalf("list code=%d stderr=%q", code, errOut)
	}
	teamAfter := readJSONFixture(t, home, ".agent-switch/profiles/claude/1/manifest.json")
	enterpriseAfter := readJSONFixture(t, home, ".agent-switch/profiles/claude/2/manifest.json")
	if teamAfter["fingerprint"] != teamBefore["fingerprint"] {
		t.Fatalf("team changed: before=%#v after=%#v", teamBefore, teamAfter)
	}
	if enterpriseAfter["fingerprint"] == enterpriseBefore["fingerprint"] {
		t.Fatalf("enterprise did not sync: before=%#v after=%#v", enterpriseBefore, enterpriseAfter)
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

func TestListUseAndRemove(t *testing.T) {
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
	if code != 0 || errOut != "" ||
		!strings.Contains(out, "List accounts") ||
		!strings.Contains(out, "1: first@example.com (active)") ||
		!strings.Contains(out, "2: second@example.com") {
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

func TestListJSONMarksActiveProfileWhenTokenFingerprintChanges(t *testing.T) {
	home := t.TempDir()
	writeJSONFixture(t, home, ".codex/auth.json", map[string]any{
		"email":        "annapo.codex@example.com",
		"access_token": "first-token",
	})
	if code, _, _ := runService(t, home, []string{"save", "--agent", "codex", "--yes"}, ""); code != 0 {
		t.Fatalf("save codex code = %d", code)
	}
	writeJSONFixture(t, home, ".codex/auth.json", map[string]any{
		"email":        "annapo.codex@example.com",
		"access_token": "refreshed-token",
	})

	code, out, errOut := runService(t, home, []string{"list", "--json"}, "")

	if code != 0 || errOut != "" {
		t.Fatalf("code=%d stderr=%q out=%q", code, errOut, out)
	}
	var profiles []map[string]any
	if err := json.Unmarshal([]byte(out), &profiles); err != nil {
		t.Fatalf("json error: %v\n%s", err, out)
	}
	if len(profiles) != 1 || profiles[0]["active"] != true {
		t.Fatalf("profiles = %#v", profiles)
	}
}

func TestListSyncsActiveDuplicateBeforeLoadingUsageMetadata(t *testing.T) {
	home := t.TempDir()
	writeJSONFixture(t, home, ".claude/.credentials.json", map[string]any{"email": "annapo@example.com", "accessToken": "expired-token"})
	if code, _, _ := runService(t, home, []string{"save", "--agent", "claude", "--yes"}, ""); code != 0 {
		t.Fatalf("save code = %d", code)
	}
	writeJSONFixture(t, home, ".claude/.credentials.json", map[string]any{"email": "annapo@example.com", "accessToken": "refreshed-token"})
	service := NewWithUsageProvider(home, profileSnapshotUsage{})
	var out bytes.Buffer

	code := service.List(&out, "claude", false)

	if code != 0 {
		t.Fatalf("code = %d\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "└ • oauth: fresh") {
		t.Fatalf("out:\n%s", out.String())
	}
	snapshot := readJSONFixture(t, home, ".agent-switch/profiles/claude/1/files/.claude/.credentials.json")
	if snapshot["accessToken"] != "refreshed-token" {
		t.Fatalf("snapshot = %+v", snapshot)
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

func TestListAddsUsageMetadata(t *testing.T) {
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

	code := service.List(&out, "", false)

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

func saveTwoClaudeFileAccounts(t *testing.T, home string) {
	t.Helper()
	writeJSONFixture(t, home, ".claude/.credentials.json", map[string]any{"email": "first@example.com", "accessToken": "first-test-token"})
	if code, _, _ := runService(t, home, []string{"save", "--agent", "claude", "--yes"}, ""); code != 0 {
		t.Fatalf("save first code = %d", code)
	}
	writeJSONFixture(t, home, ".claude/.credentials.json", map[string]any{"email": "second@example.com", "accessToken": "second-test-token"})
	if code, _, _ := runService(t, home, []string{"save", "--agent", "claude", "--yes"}, ""); code != 0 {
		t.Fatalf("save second code = %d", code)
	}
}

func TestUseClaudeMovesBackgroundSessionsToSwitchedAccount(t *testing.T) {
	home := t.TempDir()
	saveTwoClaudeFileAccounts(t, home)
	service := New(home)
	calls := 0
	service.claudeBackground = func() (claudebg.Result, error) {
		calls++
		return claudebg.Result{Moved: []string{"a1", "d4"}, Interrupted: []string{"b2"}, Failed: []string{"e5"}}, nil
	}
	var stdout, stderr bytes.Buffer

	code := service.Run([]string{"use", "1"}, strings.NewReader(""), &stdout, &stderr)

	out := stdout.String()
	if code != 0 || calls != 1 ||
		!strings.Contains(out, "Switched Claude to account #1.") ||
		!strings.Contains(out, "Moved 2 background sessions to this account.") ||
		!strings.Contains(out, "Restarted 1 busy background session on this account; in-progress work was cut off.") ||
		!strings.Contains(out, "Could not move background sessions: e5") ||
		strings.Contains(out, "This session restarts") {
		t.Fatalf("code=%d calls=%d out=\n%s", code, calls, out)
	}
}

func TestUseClaudeRestartsItsOwnSessionAfterTheRest(t *testing.T) {
	home := t.TempDir()
	saveTwoClaudeFileAccounts(t, home)
	service := New(home)
	service.claudeBackground = func() (claudebg.Result, error) {
		return claudebg.Result{Moved: []string{"a1"}, Self: "e5"}, nil
	}
	var respawned []string
	service.respawnClaudeSelf = func(id string) error {
		respawned = append(respawned, id)
		return nil
	}
	var stdout, stderr bytes.Buffer

	code := service.Run([]string{"use", "1"}, strings.NewReader(""), &stdout, &stderr)

	out := stdout.String()
	if code != 0 || strings.Join(respawned, ",") != "e5" ||
		!strings.HasSuffix(out, "This session restarts on this account in a few seconds.\n") {
		t.Fatalf("code=%d respawned=%#v out=\n%s", code, respawned, out)
	}
}

func TestUseClaudeStillSwitchesWhenBackgroundSessionsCannotMove(t *testing.T) {
	home := t.TempDir()
	saveTwoClaudeFileAccounts(t, home)
	service := New(home)
	service.claudeBackground = func() (claudebg.Result, error) {
		return claudebg.Result{}, errors.New("list background sessions: claude not found")
	}
	var stdout, stderr bytes.Buffer

	code := service.Run([]string{"use", "1"}, strings.NewReader(""), &stdout, &stderr)

	if code != 0 || !strings.Contains(stdout.String(), "Background sessions were not moved: list background sessions: claude not found") {
		t.Fatalf("code=%d out=\n%s", code, stdout.String())
	}
	if active := readJSONFixture(t, home, ".claude/.credentials.json"); active["email"] != "first@example.com" {
		t.Fatalf("active = %#v", active)
	}
}

func TestUseCodexLeavesClaudeBackgroundAlone(t *testing.T) {
	home := t.TempDir()
	writeJSONFixture(t, home, ".codex/auth.json", map[string]any{"email": "codex@example.com", "tokens": map[string]any{"access_token": "codex-token"}})
	if code, _, _ := runService(t, home, []string{"save", "--agent", "codex", "--yes"}, ""); code != 0 {
		t.Fatalf("save code = %d", code)
	}
	service := New(home)
	service.claudeBackground = func() (claudebg.Result, error) {
		t.Fatal("Claude background sessions moved for a Codex switch")
		return claudebg.Result{}, nil
	}
	var stdout, stderr bytes.Buffer

	if code := service.Run([]string{"use", "1", "--agent", "codex"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("code=%d out=\n%s", code, stdout.String())
	}
}

func TestServiceForOtherHomeNeverTouchesRealClaudeDaemon(t *testing.T) {
	if service := New(t.TempDir()); service.claudeBackground != nil || service.respawnClaudeSelf != nil {
		t.Fatal("a service for a non-current home must not restart the real Claude daemon")
	}
}

type memoryKeychain struct {
	item *provider.KeychainItem
}

func (k *memoryKeychain) IsAvailable(home string) bool { return true }
func (k *memoryKeychain) ReadItem(service string) (*provider.KeychainItem, error) {
	if k.item == nil {
		return nil, errors.New("not found")
	}
	return k.item, nil
}
func (k *memoryKeychain) WriteItem(service, account, secret string) error {
	k.item = &provider.KeychainItem{Account: account, Secret: secret}
	return nil
}

func serviceWithKeychain(home string, keychain provider.KeychainBackend) *Service {
	service := NewWithUsageProvider(home, fakeUsage{})
	service.providers = []provider.Provider{provider.NewClaudeProvider(keychain), provider.NewCodexProvider()}
	return service
}

func runWith(service *Service, args []string, input string) (int, string) {
	var stdout, stderr bytes.Buffer
	code := service.Run(args, strings.NewReader(input), &stdout, &stderr)
	return code, stdout.String() + stderr.String()
}

func TestSaveTokenStoresClaudeProfileFromStdin(t *testing.T) {
	home := t.TempDir()
	service := serviceWithKeychain(home, &memoryKeychain{})

	code, out := runWith(service, []string{"save", "--token", "--label", "bot"}, "sk-ant-oat01-bot\n")

	if code != 0 || !strings.Contains(out, "Saved claude account #1 (token: bot)") {
		t.Fatalf("code=%d out=\n%s", code, out)
	}
	manifest := readJSONFixture(t, home, ".agent-switch/profiles/claude/1/manifest.json")
	snapshot := readJSONFixture(t, home, ".agent-switch/profiles/claude/1/keychain.json")
	if manifest["label"] != "bot" || !strings.Contains(snapshot["secret"].(string), `"accessToken":"sk-ant-oat01-bot"`) {
		t.Fatalf("manifest=%#v snapshot=%#v", manifest, snapshot)
	}
}

func TestSaveTokenAgainReplacesTheSameLabelProfile(t *testing.T) {
	home := t.TempDir()
	service := serviceWithKeychain(home, &memoryKeychain{})
	runWith(service, []string{"save", "--token", "--label", "bot"}, "sk-ant-oat01-old\n")

	code, out := runWith(service, []string{"save", "--token", "--label", "Bot"}, "sk-ant-oat01-new\n")

	if code != 0 || !strings.Contains(out, "Updated claude account #1 (token: Bot)") {
		t.Fatalf("code=%d out=\n%s", code, out)
	}
	snapshot := readJSONFixture(t, home, ".agent-switch/profiles/claude/1/keychain.json")
	if !strings.Contains(snapshot["secret"].(string), "sk-ant-oat01-new") {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if _, err := os.Stat(filepath.Join(home, ".agent-switch/profiles/claude/2")); err == nil {
		t.Fatal("a second profile was created")
	}
}

func TestSaveTokenRejectsMissingLabelOrToken(t *testing.T) {
	home := t.TempDir()
	service := serviceWithKeychain(home, &memoryKeychain{})

	if code, out := runWith(service, []string{"save", "--token"}, "sk-ant-oat01-bot\n"); code != 2 || !strings.Contains(out, "--label") {
		t.Fatalf("missing label code=%d out=\n%s", code, out)
	}
	if code, out := runWith(service, []string{"save", "--token", "--label", "bot"}, "\n"); code != 2 || !strings.Contains(out, "claude setup-token") {
		t.Fatalf("missing token code=%d out=\n%s", code, out)
	}
	if code, out := runWith(service, []string{"save", "--token", "--label", "bot", "--agent", "codex"}, "sk-ant-oat01-bot\n"); code != 2 || !strings.Contains(out, "Claude") {
		t.Fatalf("codex token code=%d out=\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(home, ".agent-switch/profiles/claude")); err == nil {
		t.Fatal("a profile was saved")
	}
}

func TestTokenProfileNeverOverwritesTheOrganizationProfile(t *testing.T) {
	home := t.TempDir()
	annapoSecret := `{"claudeAiOauth":{"accessToken":"annapo-access","refreshToken":"annapo-refresh"}}`
	keychain := &memoryKeychain{item: &provider.KeychainItem{Account: "annapo", Secret: annapoSecret}}
	writeJSONFixture(t, home, ".claude.json", map[string]any{"oauthAccount": map[string]any{
		"emailAddress":     "annapo@example.com",
		"accountUuid":      "account-annapo",
		"organizationUuid": "org-team",
		"organizationName": "Team",
	}})
	service := serviceWithKeychain(home, keychain)
	if code, out := runWith(service, []string{"save", "--agent", "claude", "--yes"}, ""); code != 0 {
		t.Fatalf("save annapo code=%d out=\n%s", code, out)
	}
	if code, out := runWith(service, []string{"save", "--token", "--label", "bot"}, "sk-ant-oat01-bot\n"); code != 0 {
		t.Fatalf("save token code=%d out=\n%s", code, out)
	}

	if code, out := runWith(service, []string{"use", "2"}, ""); code != 0 {
		t.Fatalf("use token code=%d out=\n%s", code, out)
	}
	if !strings.Contains(keychain.item.Secret, "sk-ant-oat01-bot") {
		t.Fatalf("keychain = %#v", keychain.item)
	}
	if code, out := runWith(service, []string{"list"}, ""); code != 0 || !strings.Contains(out, "2: bot (active)") {
		t.Fatalf("list code=%d out=\n%s", code, out)
	}
	keychain.item.Secret = strings.Replace(keychain.item.Secret, `"subscriptionType":null`, `"subscriptionType":"max"`, 1)
	runWith(service, []string{"list"}, "")

	annapoSnapshot := readJSONFixture(t, home, ".agent-switch/profiles/claude/1/keychain.json")
	tokenSnapshot := readJSONFixture(t, home, ".agent-switch/profiles/claude/2/keychain.json")
	if annapoSnapshot["secret"] != annapoSecret {
		t.Fatalf("organization profile was overwritten: %#v", annapoSnapshot)
	}
	if !strings.Contains(tokenSnapshot["secret"].(string), `"max"`) {
		t.Fatalf("token profile did not keep its latest credential: %#v", tokenSnapshot)
	}

	if code, out := runWith(service, []string{"use", "1"}, ""); code != 0 {
		t.Fatalf("use annapo code=%d out=\n%s", code, out)
	}
	config := readJSONFixture(t, home, ".claude.json")
	if keychain.item.Secret != annapoSecret || config["oauthAccount"].(map[string]any)["organizationUuid"] != "org-team" {
		t.Fatalf("keychain=%#v config=%#v", keychain.item, config)
	}
}

func TestRenameShowsNameAndKeepsItWhenCredentialsRotate(t *testing.T) {
	home := t.TempDir()
	writeJSONFixture(t, home, ".claude/.credentials.json", map[string]any{"email": "first@example.com", "accessToken": "first-token"})
	if code, _, _ := runService(t, home, []string{"save", "--agent", "claude", "--yes"}, ""); code != 0 {
		t.Fatalf("save code = %d", code)
	}

	code, out, errOut := runService(t, home, []string{"rename", "1", "annapo"}, "")
	if code != 0 || errOut != "" || !strings.Contains(out, "Renamed claude account #1 to annapo") {
		t.Fatalf("rename code=%d err=%q out=\n%s", code, errOut, out)
	}
	writeJSONFixture(t, home, ".claude/.credentials.json", map[string]any{"email": "first@example.com", "accessToken": "rotated-token"})

	_, out, _ = runService(t, home, []string{"list"}, "")
	if !strings.Contains(out, "1: annapo (active)") || strings.Contains(out, "first@example.com") {
		t.Fatalf("list out=\n%s", out)
	}
	manifest := readJSONFixture(t, home, ".agent-switch/profiles/claude/1/manifest.json")
	if manifest["label"] != "first@example.com" || manifest["name"] != "annapo" {
		t.Fatalf("manifest = %#v", manifest)
	}
}

func TestRenameRejectsUnknownAccountOrMissingName(t *testing.T) {
	home := t.TempDir()
	if code, out, _ := runService(t, home, []string{"rename", "7", "annapo"}, ""); code != 1 || !strings.Contains(out, "No saved account matches #7.") {
		t.Fatalf("unknown code=%d out=\n%s", code, out)
	}
	if code, _, errOut := runService(t, home, []string{"rename", "1"}, ""); code != 2 || !strings.Contains(errOut, "missing name") {
		t.Fatalf("missing name code=%d err=%q", code, errOut)
	}
}

func TestCodexAPIKeyProfilesStaySeparate(t *testing.T) {
	home := t.TempDir()
	for _, key := range []string{"key-a", "key-b"} {
		writeJSONFixture(t, home, ".codex/auth.json", map[string]any{"OPENAI_API_KEY": key})
		if code, out, _ := runService(t, home, []string{"save", "--agent", "codex", "--yes"}, ""); code != 0 || !strings.Contains(out, "Saved codex account") {
			t.Fatalf("save %s code=%d out=\n%s", key, code, out)
		}
	}
	writeJSONFixture(t, home, ".codex/auth.json", map[string]any{"OPENAI_API_KEY": "key-c"})

	if code, _, _ := runService(t, home, []string{"list", "--agent", "codex"}, ""); code != 0 {
		t.Fatalf("list code = %d", code)
	}

	for number, key := range map[string]string{"1": "key-a", "2": "key-b"} {
		snapshot := readJSONFixture(t, home, ".agent-switch/profiles/codex/"+number+"/files/.codex/auth.json")
		if snapshot["OPENAI_API_KEY"] != key {
			t.Fatalf("profile %s = %#v", number, snapshot)
		}
	}
}

func TestUseSavesTheCurrentLoginBeforeSwitching(t *testing.T) {
	home := t.TempDir()
	saveTwoClaudeFileAccounts(t, home)
	writeJSONFixture(t, home, ".claude/.credentials.json", map[string]any{"email": "second@example.com", "accessToken": "second-rotated-token"})

	if code, out, _ := runService(t, home, []string{"use", "1", "--agent", "claude"}, ""); code != 0 {
		t.Fatalf("use code=%d out=\n%s", code, out)
	}

	snapshot := readJSONFixture(t, home, ".agent-switch/profiles/claude/2/files/.claude/.credentials.json")
	if snapshot["accessToken"] != "second-rotated-token" {
		t.Fatalf("outgoing login was not saved: %#v", snapshot)
	}
	if active := readJSONFixture(t, home, ".claude/.credentials.json"); active["email"] != "first@example.com" {
		t.Fatalf("active = %#v", active)
	}
}

func TestUseStopsWhenSavingTheCurrentLoginRemovesTheTarget(t *testing.T) {
	home := t.TempDir()
	identity := map[string]any{"account_uuid": "account-1", "organization_uuid": "team-org"}
	for index, dir := range []string{"1", "2"} {
		writeJSONFixture(t, home, ".agent-switch/profiles/claude/"+dir+"/manifest.json", map[string]any{
			"agent": "claude", "display_name": "Claude", "number": index + 1, "label": "same@example.com",
			"fingerprint": "old-" + dir, "metadata": identity,
		})
	}
	writeJSONFixture(t, home, ".claude.json", map[string]any{"oauthAccount": map[string]any{
		"emailAddress": "same@example.com", "accountUuid": "account-1", "organizationUuid": "team-org",
	}})
	current := `{"claudeAiOauth":{"accessToken":"current-access","refreshToken":"current-refresh"}}`
	keychain := &memoryKeychain{item: &provider.KeychainItem{Account: "annapo", Secret: current}}
	service := serviceWithKeychain(home, keychain)

	code, out := runWith(service, []string{"use", "2"}, "")

	if code != 1 || strings.Contains(out, "Switched") || !strings.Contains(out, "No saved account matches #2.") {
		t.Fatalf("code=%d out=\n%s", code, out)
	}
	if keychain.item.Secret != current {
		t.Fatalf("keychain = %#v", keychain.item)
	}
}

func saveBrowserLoginAndTokenProfile(t *testing.T, home string) (*Service, *memoryKeychain, string) {
	t.Helper()
	annapoSecret := `{"claudeAiOauth":{"accessToken":"annapo-access","refreshToken":"annapo-refresh"}}`
	keychain := &memoryKeychain{item: &provider.KeychainItem{Account: "annapo", Secret: annapoSecret}}
	writeJSONFixture(t, home, ".claude.json", map[string]any{"oauthAccount": map[string]any{
		"emailAddress":     "annapo@example.com",
		"accountUuid":      "account-annapo",
		"organizationUuid": "org-team",
		"organizationName": "Team",
	}})
	service := serviceWithKeychain(home, keychain)
	if code, out := runWith(service, []string{"save", "--agent", "claude", "--yes"}, ""); code != 0 {
		t.Fatalf("save annapo code=%d out=\n%s", code, out)
	}
	if code, out := runWith(service, []string{"save", "--token", "--label", "bot"}, "sk-ant-oat01-bot\n"); code != 0 {
		t.Fatalf("save token code=%d out=\n%s", code, out)
	}
	return service, keychain, annapoSecret
}

func TestBrowserLoginSurvivesWhenClaudeConfigLagsBehindTokenSwitch(t *testing.T) {
	home := t.TempDir()
	service, keychain, annapoSecret := saveBrowserLoginAndTokenProfile(t, home)
	annapoConfig := readJSONFixture(t, home, ".claude.json")
	if code, out := runWith(service, []string{"use", "2"}, ""); code != 0 {
		t.Fatalf("use token code=%d out=\n%s", code, out)
	}
	writeJSONFixture(t, home, ".claude.json", annapoConfig)

	if code, out := runWith(service, []string{"list"}, ""); code != 0 || !strings.Contains(out, "2: bot (active)") {
		t.Fatalf("list code=%d out=\n%s", code, out)
	}
	keychain.item.Secret = strings.Replace(keychain.item.Secret, `"subscriptionType":null`, `"subscriptionType":"max"`, 1)
	runWith(service, []string{"list"}, "")
	runWith(service, []string{"save", "--agent", "claude", "--yes"}, "")

	annapoSnapshot := readJSONFixture(t, home, ".agent-switch/profiles/claude/1/keychain.json")
	if annapoSnapshot["secret"] != annapoSecret {
		t.Fatalf("browser login was overwritten: %#v", annapoSnapshot)
	}
	tokenManifest := readJSONFixture(t, home, ".agent-switch/profiles/claude/2/manifest.json")
	tokenSnapshot := readJSONFixture(t, home, ".agent-switch/profiles/claude/2/keychain.json")
	tokenConfig := readJSONFixture(t, home, ".agent-switch/profiles/claude/2/config.json")
	if tokenManifest["label"] != "bot" || !strings.Contains(tokenSnapshot["secret"].(string), `"max"`) ||
		len(tokenConfig["oauthAccount"].(map[string]any)) != 1 || tokenConfig["oauthAccount"].(map[string]any)["emailAddress"] != "bot" {
		t.Fatalf("token profile took on the browser login: manifest=%#v config=%#v", tokenManifest, tokenConfig)
	}
	if _, err := os.Stat(filepath.Join(home, ".agent-switch/profiles/claude/3")); err == nil {
		t.Fatal("the mismatched login was saved as a third profile")
	}
}

func TestTokenProfileSurvivesWhenClaudeConfigLagsBehindBrowserLogin(t *testing.T) {
	home := t.TempDir()
	service, keychain, _ := saveBrowserLoginAndTokenProfile(t, home)
	if code, out := runWith(service, []string{"use", "2"}, ""); code != 0 {
		t.Fatalf("use token code=%d out=\n%s", code, out)
	}
	keychain.item.Secret = `{"claudeAiOauth":{"accessToken":"other-access","refreshToken":"other-refresh"}}`

	code, out := runWith(service, []string{"list"}, "")

	if code != 0 || strings.Contains(out, "(active)") {
		t.Fatalf("list code=%d out=\n%s", code, out)
	}
	tokenSnapshot := readJSONFixture(t, home, ".agent-switch/profiles/claude/2/keychain.json")
	if !strings.Contains(tokenSnapshot["secret"].(string), "sk-ant-oat01-bot") {
		t.Fatalf("token profile was overwritten: %#v", tokenSnapshot)
	}
}

func TestOtherOrganizationSurvivesWhenClaudeConfigLagsBehind(t *testing.T) {
	home := t.TempDir()
	keychain := &memoryKeychain{}
	service := serviceWithKeychain(home, keychain)
	secrets := map[string]string{
		"team-org":       `{"claudeAiOauth":{"accessToken":"team-access","refreshToken":"team-refresh"}}`,
		"enterprise-org": `{"claudeAiOauth":{"accessToken":"enterprise-access","refreshToken":"enterprise-refresh"}}`,
	}
	signIn := func(organization string) {
		writeJSONFixture(t, home, ".claude.json", map[string]any{"oauthAccount": map[string]any{
			"emailAddress": "same@example.com", "accountUuid": "account-1", "organizationUuid": organization,
		}})
	}
	for _, organization := range []string{"team-org", "enterprise-org"} {
		keychain.item = &provider.KeychainItem{Account: "annapo", Secret: secrets[organization]}
		signIn(organization)
		if code, out := runWith(service, []string{"save", "--agent", "claude", "--yes"}, ""); code != 0 {
			t.Fatalf("save %s code=%d out=\n%s", organization, code, out)
		}
	}
	signIn("team-org")

	code, out := runWith(service, []string{"list"}, "")
	runWith(service, []string{"save", "--agent", "claude", "--yes"}, "")

	if code != 0 || !strings.Contains(out, "2: same@example.com (active)") || strings.Contains(out, "1: same@example.com (active)") {
		t.Fatalf("list code=%d out=\n%s", code, out)
	}
	if snapshot := readJSONFixture(t, home, ".agent-switch/profiles/claude/1/keychain.json"); snapshot["secret"] != secrets["team-org"] {
		t.Fatalf("team login was overwritten: %#v", snapshot)
	}
}

func TestTokenProfilesStayApartWhenClaudeConfigLagsBehind(t *testing.T) {
	home := t.TempDir()
	keychain := &memoryKeychain{}
	service := serviceWithKeychain(home, keychain)
	for label, token := range map[string]string{"first": "sk-ant-oat01-first", "second": "sk-ant-oat01-second"} {
		if code, out := runWith(service, []string{"save", "--token", "--label", label}, token+"\n"); code != 0 {
			t.Fatalf("save %s code=%d out=\n%s", label, code, out)
		}
	}
	numbers := map[string]string{}
	for _, number := range []string{"1", "2"} {
		numbers[readJSONFixture(t, home, ".agent-switch/profiles/claude/"+number+"/manifest.json")["label"].(string)] = number
	}
	if code, out := runWith(service, []string{"use", numbers["second"]}, ""); code != 0 {
		t.Fatalf("use second code=%d out=\n%s", code, out)
	}
	writeJSONFixture(t, home, ".claude.json", map[string]any{"oauthAccount": map[string]any{"emailAddress": "first"}})
	keychain.item.Secret = strings.Replace(keychain.item.Secret, `"subscriptionType":null`, `"subscriptionType":"max"`, 1)

	if code, out := runWith(service, []string{"use", numbers["first"]}, ""); code != 0 {
		t.Fatalf("use first code=%d out=\n%s", code, out)
	}

	first := readJSONFixture(t, home, ".agent-switch/profiles/claude/"+numbers["first"]+"/keychain.json")["secret"].(string)
	second := readJSONFixture(t, home, ".agent-switch/profiles/claude/"+numbers["second"]+"/keychain.json")["secret"].(string)
	secondManifest := readJSONFixture(t, home, ".agent-switch/profiles/claude/"+numbers["second"]+"/manifest.json")
	if !strings.Contains(first, "sk-ant-oat01-first") || strings.Contains(first, `"max"`) {
		t.Fatalf("first token was overwritten: %s", first)
	}
	if !strings.Contains(second, "sk-ant-oat01-second") || !strings.Contains(second, `"max"`) || secondManifest["label"] != "second" {
		t.Fatalf("second token did not keep its own login: %s manifest=%#v", second, secondManifest)
	}
	if !strings.Contains(keychain.item.Secret, "sk-ant-oat01-first") {
		t.Fatalf("keychain = %#v", keychain.item)
	}
}

func TestSaveTokenKeepsBrowserLoginWithTheSameLabel(t *testing.T) {
	home := t.TempDir()
	annapoSecret := `{"claudeAiOauth":{"accessToken":"annapo-access","refreshToken":"annapo-refresh"}}`
	service := serviceWithKeychain(home, &memoryKeychain{item: &provider.KeychainItem{Account: "annapo", Secret: annapoSecret}})
	if code, out := runWith(service, []string{"save", "--agent", "claude", "--yes"}, ""); code != 0 {
		t.Fatalf("save annapo code=%d out=\n%s", code, out)
	}

	code, out := runWith(service, []string{"save", "--token", "--label", "annapo"}, "sk-ant-oat01-annapo\n")

	if code != 0 || !strings.Contains(out, "Saved claude account #2 (token: annapo)") {
		t.Fatalf("code=%d out=\n%s", code, out)
	}
	if snapshot := readJSONFixture(t, home, ".agent-switch/profiles/claude/1/keychain.json"); snapshot["secret"] != annapoSecret {
		t.Fatalf("browser login was overwritten: %#v", snapshot)
	}
}

func TestSaveTokenUnderAnotherLabelRenamesItsProfile(t *testing.T) {
	home := t.TempDir()
	service := serviceWithKeychain(home, &memoryKeychain{})
	runWith(service, []string{"save", "--token", "--label", "bot"}, "sk-ant-oat01-bot\n")

	code, out := runWith(service, []string{"save", "--token", "--label", "robot"}, "sk-ant-oat01-bot\n")

	if code != 0 || !strings.Contains(out, "Updated claude account #1 (token: robot)") {
		t.Fatalf("code=%d out=\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(home, ".agent-switch/profiles/claude/2")); err == nil {
		t.Fatal("the same token was saved twice")
	}
}

func TestSaveTokenRejectsOtherSecrets(t *testing.T) {
	home := t.TempDir()
	service := serviceWithKeychain(home, &memoryKeychain{})

	for _, input := range []string{"ghp_notaclaudetoken\n", "sk-proj-notaclaudetoken\n"} {
		if code, out := runWith(service, []string{"save", "--token", "--label", "bot"}, input); code != 2 || !strings.Contains(out, "claude setup-token") {
			t.Fatalf("input %q code=%d out=\n%s", input, code, out)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".agent-switch/profiles/claude")); err == nil {
		t.Fatal("a profile was saved")
	}
}

func TestSaveTokenRejectsLabelThatNamesNothing(t *testing.T) {
	home := t.TempDir()
	service := serviceWithKeychain(home, &memoryKeychain{})

	if code, out := runWith(service, []string{"save", "--token", "--label", "Unknown"}, "sk-ant-oat01-bot\n"); code != 2 || !strings.Contains(out, "--label") {
		t.Fatalf("code=%d out=\n%s", code, out)
	}
}

func TestMCPLoginsStayWithTheMachineAcrossSwitches(t *testing.T) {
	home := t.TempDir()
	service, keychain, annapoSecret := saveBrowserLoginAndTokenProfile(t, home)
	keychain.item.Secret = strings.TrimSuffix(annapoSecret, "}") + `,"mcpOAuth":{"notion":{"refreshToken":"mcp-1"}}}`
	if code, out := runWith(service, []string{"list"}, ""); code != 0 || strings.Contains(out, "3:") {
		t.Fatalf("an MCP login looked like a new account: code=%d out=\n%s", code, out)
	}

	if code, out := runWith(service, []string{"use", "2"}, ""); code != 0 {
		t.Fatalf("use token code=%d out=\n%s", code, out)
	}
	if !strings.Contains(keychain.item.Secret, "sk-ant-oat01-bot") || !strings.Contains(keychain.item.Secret, "mcp-1") {
		t.Fatalf("switching dropped the MCP login: %s", keychain.item.Secret)
	}
	keychain.item.Secret = strings.Replace(keychain.item.Secret, "mcp-1", "mcp-2", 1)
	keychain.item.Secret = strings.Replace(keychain.item.Secret, `"subscriptionType":null`, `"subscriptionType":"max"`, 1)
	if code, out := runWith(service, []string{"list"}, ""); code != 0 {
		t.Fatalf("list code=%d out=\n%s", code, out)
	}
	if secret := readJSONFixture(t, home, ".agent-switch/profiles/claude/2/keychain.json")["secret"].(string); !strings.Contains(secret, `"max"`) {
		t.Fatalf("token profile did not keep its latest login: %s", secret)
	}
	if code, out := runWith(service, []string{"use", "1"}, ""); code != 0 {
		t.Fatalf("use browser code=%d out=\n%s", code, out)
	}
	if !strings.Contains(keychain.item.Secret, "annapo-refresh") || !strings.Contains(keychain.item.Secret, "mcp-2") || strings.Contains(keychain.item.Secret, "mcp-1") {
		t.Fatalf("switching back restored an old MCP login: %s", keychain.item.Secret)
	}
	for _, number := range []string{"1", "2"} {
		if secret := readJSONFixture(t, home, ".agent-switch/profiles/claude/"+number+"/keychain.json")["secret"].(string); strings.Contains(secret, "mcp") {
			t.Fatalf("profile %s saved an MCP login: %s", number, secret)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".agent-switch/profiles/claude/3")); err == nil {
		t.Fatal("an MCP login was saved as a third profile")
	}
}

func TestOldProfileWithMCPLoginsTakesTheCurrentOnes(t *testing.T) {
	home := t.TempDir()
	service, keychain, _ := saveBrowserLoginAndTokenProfile(t, home)
	snapshotPath := ".agent-switch/profiles/claude/2/keychain.json"
	snapshot := readJSONFixture(t, home, snapshotPath)
	snapshot["secret"] = strings.TrimSuffix(snapshot["secret"].(string), "}") + `,"mcpOAuth":{"notion":{"refreshToken":"stale"}}}`
	writeJSONFixture(t, home, snapshotPath, snapshot)
	manifestPath := ".agent-switch/profiles/claude/2/manifest.json"
	manifest := readJSONFixture(t, home, manifestPath)
	manifest["fingerprint"] = "fingerprint-of-the-whole-item"
	writeJSONFixture(t, home, manifestPath, manifest)
	keychain.item.Secret = strings.TrimSuffix(keychain.item.Secret, "}") + `,"mcpOAuth":{"notion":{"refreshToken":"fresh"}}}`

	if code, out := runWith(service, []string{"use", "2"}, ""); code != 0 {
		t.Fatalf("use token code=%d out=\n%s", code, out)
	}
	if !strings.Contains(keychain.item.Secret, "sk-ant-oat01-bot") || !strings.Contains(keychain.item.Secret, "fresh") || strings.Contains(keychain.item.Secret, "stale") {
		t.Fatalf("keychain = %s", keychain.item.Secret)
	}
	if code, out := runWith(service, []string{"list"}, ""); code != 0 || !strings.Contains(out, "2: bot (active)") || strings.Contains(out, "3:") {
		t.Fatalf("list code=%d out=\n%s", code, out)
	}
}

func TestRecordUsageSavesStatusLineUsageForTheActiveToken(t *testing.T) {
	for _, name := range []string{"CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CONFIG_DIR"} {
		t.Setenv(name, "")
	}
	home := t.TempDir()
	service, _, _ := saveBrowserLoginAndTokenProfile(t, home)
	statusLine := func(session string, apiMS int, fiveHour float64) string {
		return fmt.Sprintf(`{"session_id":%q,"cost":{"total_api_duration_ms":%d},"rate_limits":{"five_hour":{"used_percentage":%v,"resets_at":1783490862},"seven_day":{"used_percentage":41}}}`, session, apiMS, fiveHour)
	}
	record := func(input string) {
		t.Helper()
		if code, out := runWith(service, []string{"record-usage"}, input); code != 0 || out != "" {
			t.Fatalf("record-usage code=%d out=\n%s", code, out)
		}
	}
	usagePath := func(number string) string {
		return filepath.Join(home, ".agent-switch/profiles/claude", number, "usage.json")
	}
	fiveHour := func(number string) any {
		recorded := readJSONFromPath(usagePath(number))
		window, _ := recorded["usage"].(map[string]any)["five_hour"].(map[string]any)
		return window["utilization"]
	}

	record(statusLine("browser", 100, 10))
	record(statusLine("browser", 200, 11))
	if code, out := runWith(service, []string{"use", "2"}, ""); code != 0 {
		t.Fatalf("use token code=%d out=\n%s", code, out)
	}
	record(statusLine("browser", 200, 11))
	record(statusLine("browser", 300, 12))
	for _, number := range []string{"1", "2"} {
		if _, err := os.Stat(usagePath(number)); err == nil {
			t.Fatalf("a browser login or a session from before the switch was recorded into profile %s", number)
		}
	}

	record(statusLine("browser", 400, 23.5))
	if fiveHour("2") != 23.5 {
		t.Fatalf("token usage = %#v", readJSONFromPath(usagePath("2")))
	}
	record(`{"session_id":"browser","cost":{"total_api_duration_ms":500}}`)
	if fiveHour("2") != 23.5 {
		t.Fatal("a status line without rate limits changed the recorded usage")
	}

	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "sk-ant-oat01-other")
	record(statusLine("env", 100, 90))
	record(statusLine("env", 200, 91))
	if fiveHour("2") != 23.5 {
		t.Fatal("a session on another token was recorded into the Keychain token's profile")
	}
	if _, err := os.Stat(usagePath("1")); err == nil {
		t.Fatal("token usage was recorded into the browser profile")
	}
}

func TestOldProfileMCPLoginsAreNeverRestored(t *testing.T) {
	home := t.TempDir()
	service, keychain, _ := saveBrowserLoginAndTokenProfile(t, home)
	snapshotPath := ".agent-switch/profiles/claude/2/keychain.json"
	snapshot := readJSONFixture(t, home, snapshotPath)
	snapshot["secret"] = strings.TrimSuffix(snapshot["secret"].(string), "}") + `,"mcpOAuth":{"notion":{"refreshToken":"stale"}}}`
	writeJSONFixture(t, home, snapshotPath, snapshot)

	if code, out := runWith(service, []string{"use", "2"}, ""); code != 0 {
		t.Fatalf("use token code=%d out=\n%s", code, out)
	}
	if !strings.Contains(keychain.item.Secret, "sk-ant-oat01-bot") || strings.Contains(keychain.item.Secret, "mcpOAuth") {
		t.Fatalf("keychain = %s", keychain.item.Secret)
	}
}

func TestCommandsWaitForAnotherAgsCommand(t *testing.T) {
	home := t.TempDir()
	service, _, _ := saveBrowserLoginAndTokenProfile(t, home)
	unlock, err := service.store.Lock(0)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	original := commandLockWait
	commandLockWait = 50 * time.Millisecond
	defer func() { commandLockWait = original }()

	if code, out := runWith(service, []string{"use", "2"}, ""); code != 1 || !strings.Contains(out, "another ags command is running") {
		t.Fatalf("use code=%d out=\n%s", code, out)
	}
	if code, out := runWith(service, []string{"record-usage"}, `{"session_id":"s"}`); code != 0 || out != "" {
		t.Fatalf("record-usage code=%d out=\n%s", code, out)
	}
}

type changingKeychain struct {
	memoryKeychain
	reads int
}

func (k *changingKeychain) ReadItem(service string) (*provider.KeychainItem, error) {
	k.reads++
	if k.reads > 1 {
		return &provider.KeychainItem{Account: "annapo", Secret: `{"claudeAiOauth":{"accessToken":"changed","refreshToken":"changed"}}`}, nil
	}
	return k.memoryKeychain.ReadItem(service)
}

func TestFailedSaveLeavesNoProfileBehind(t *testing.T) {
	home := t.TempDir()
	keychain := &changingKeychain{memoryKeychain: memoryKeychain{item: &provider.KeychainItem{Account: "annapo", Secret: `{"claudeAiOauth":{"accessToken":"a","refreshToken":"r"}}`}}}
	service := serviceWithKeychain(home, keychain)

	if code, out := runWith(service, []string{"save", "--agent", "claude", "--yes"}, ""); code == 0 || !strings.Contains(out, "credentials changed") {
		t.Fatalf("save code=%d out=\n%s", code, out)
	}
	if entries, _ := os.ReadDir(filepath.Join(home, ".agent-switch/profiles/claude")); len(entries) != 0 {
		t.Fatalf("a half-saved profile was left: %v", entries)
	}
}

func TestUseRefusesAProfileWithoutSavedCredentials(t *testing.T) {
	home := t.TempDir()
	saveTwoClaudeFileAccounts(t, home)
	if err := os.RemoveAll(filepath.Join(home, ".agent-switch/profiles/claude/1/files")); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(home, ".claude/.credentials.json"))

	code, out, _ := runService(t, home, []string{"use", "1"}, "")

	after, _ := os.ReadFile(filepath.Join(home, ".claude/.credentials.json"))
	if code != 1 || strings.Contains(out, "Switched") || string(before) != string(after) {
		t.Fatalf("code=%d out=\n%s", code, out)
	}
}
