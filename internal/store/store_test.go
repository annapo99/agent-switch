package store

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/annapo99/agent-switch/internal/identity"
	"github.com/annapo99/agent-switch/internal/model"
)

func active(agent, label, fingerprint string, metadata model.Metadata) model.ActiveAccount {
	display := map[string]string{"claude": "Claude", "codex": "Codex"}[agent]
	if display == "" {
		display = agent
	}
	return model.ActiveAccount{
		Agent:       agent,
		DisplayName: display,
		Label:       label,
		Fingerprint: fingerprint,
		Source:      "fixture",
		AuthFiles:   []string{},
		Metadata:    metadata,
	}
}

func writeStoreJSON(t *testing.T, path string, payload any) {
	t.Helper()
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

func TestNextNumberIsScopedPerAgent(t *testing.T) {
	s := New(t.TempDir())
	if _, err := s.CreateProfile(active("claude", "a@example.com", "fp-a", nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProfile(active("claude", "b@example.com", "fp-b", nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProfile(active("codex", "c@example.com", "fp-c", nil)); err != nil {
		t.Fatal(err)
	}

	if got := s.NextNumber("claude"); got != 3 {
		t.Fatalf("claude next = %d", got)
	}
	if got := s.NextNumber("codex"); got != 2 {
		t.Fatalf("codex next = %d", got)
	}
	if got := s.NextNumber("gemini"); got != 1 {
		t.Fatalf("gemini next = %d", got)
	}
}

func TestCreateProfilePersistsManifestAndMetadata(t *testing.T) {
	home := t.TempDir()
	s := New(home)
	profile, err := s.CreateProfile(active("claude", "a@example.com", "fp-a", model.Metadata{
		"organization_name": "Example Team",
	}))
	if err != nil {
		t.Fatal(err)
	}

	if profile.Agent != "claude" || profile.Number != 1 {
		t.Fatalf("profile = %+v", profile)
	}
	data, err := os.ReadFile(filepath.Join(home, ".agent-switch/profiles/claude/1/manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest model.Profile
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Label != "a@example.com" || manifest.Fingerprint != "fp-a" {
		t.Fatalf("manifest = %+v", manifest)
	}
	if got := manifest.Metadata.String("organization_name"); got != "Example Team" {
		t.Fatalf("organization metadata = %q", got)
	}
}

func TestFindDuplicateUsesAgentAndStableIdentity(t *testing.T) {
	s := New(t.TempDir())
	if _, err := s.CreateProfile(active("claude", "a@example.com", "same", nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProfile(active("codex", "a@example.com", "same", nil)); err != nil {
		t.Fatal(err)
	}

	claude, ok := s.FindDuplicate(active("claude", "other@example.com", "same", nil))
	if !ok || claude.Agent != "claude" || claude.Number != 1 {
		t.Fatalf("claude duplicate = %+v ok=%v", claude, ok)
	}
	codex, ok := s.FindDuplicate(active("codex", "other@example.com", "same", nil))
	if !ok || codex.Agent != "codex" || codex.Number != 1 {
		t.Fatalf("codex duplicate = %+v ok=%v", codex, ok)
	}
	rotated, ok := s.FindDuplicate(active("claude", "A@EXAMPLE.COM", "rotated-fingerprint", nil))
	if !ok || rotated.Agent != "claude" || rotated.Number != 1 {
		t.Fatalf("rotated duplicate = %+v ok=%v", rotated, ok)
	}
}

func TestFindDuplicatePrefersFingerprintBeforeLabelFallback(t *testing.T) {
	s := New(t.TempDir())
	if _, err := s.CreateProfile(active("claude", "stale@example.com", "fp-stale", nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProfile(active("claude", "target@example.com", "fp-target", nil)); err != nil {
		t.Fatal(err)
	}

	profile, ok := s.FindDuplicate(active("claude", "stale@example.com", "fp-target", nil))

	if !ok || profile.Number != 2 {
		t.Fatalf("duplicate = %+v ok=%v", profile, ok)
	}
}

func TestFindDuplicateSeparatesClaudeOrganizationsWithSameFingerprint(t *testing.T) {
	s := New(t.TempDir())
	team := active("claude", "same@example.com", "shared-token", model.Metadata{
		model.MetadataAccountUUID:      "account-1",
		model.MetadataOrganizationUUID: "team-org",
	})
	if _, err := s.CreateProfile(team); err != nil {
		t.Fatal(err)
	}
	enterprise := active("claude", "same@example.com", "shared-token", model.Metadata{
		model.MetadataAccountUUID:      "account-1",
		model.MetadataOrganizationUUID: "enterprise-org",
	})

	if duplicate, ok := s.FindDuplicate(enterprise); ok {
		t.Fatalf("unexpected duplicate = %+v", duplicate)
	}
}

func TestFindDuplicateMatchesClaudeOrganizationAfterTokenRotation(t *testing.T) {
	s := New(t.TempDir())
	team := active("claude", "same@example.com", "old-token", model.Metadata{
		model.MetadataAccountUUID:      "account-1",
		model.MetadataOrganizationUUID: "team-org",
	})
	if _, err := s.CreateProfile(team); err != nil {
		t.Fatal(err)
	}
	rotated := active("claude", "same@example.com", "new-token", model.Metadata{
		model.MetadataAccountUUID:      "account-1",
		model.MetadataOrganizationUUID: "team-org",
	})

	duplicate, ok := s.FindDuplicate(rotated)
	if !ok || duplicate.Number != 1 {
		t.Fatalf("duplicate = %+v ok=%v", duplicate, ok)
	}
}

func TestListProfilesBackfillsClaudeOrganizationIdentityFromSnapshot(t *testing.T) {
	home := t.TempDir()
	s := New(home)
	profile, err := s.CreateProfile(active("claude", "same@example.com", "token", model.Metadata{
		model.MetadataOrganizationName: "Example Team",
	}))
	if err != nil {
		t.Fatal(err)
	}
	profileDir := s.ProfileDir("claude", profile.Number)
	writeStoreJSON(t, filepath.Join(profileDir, "config.json"), map[string]any{
		"oauthAccount": map[string]any{
			"accountUuid":      "account-1",
			"organizationUuid": "team-org",
		},
	})

	loaded := s.ListProfiles("claude")
	if len(loaded) != 1 ||
		loaded[0].Metadata.String(model.MetadataAccountUUID) != "account-1" ||
		loaded[0].Metadata.String(model.MetadataOrganizationUUID) != "team-org" {
		t.Fatalf("profiles = %+v", loaded)
	}
	data, err := os.ReadFile(filepath.Join(profileDir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var persisted model.Profile
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.Metadata.String(model.MetadataOrganizationUUID) != "team-org" {
		t.Fatalf("persisted = %+v", persisted)
	}
}

func TestListProfilesLeavesManifestUntouchedForMalformedClaudeSnapshot(t *testing.T) {
	home := t.TempDir()
	s := New(home)
	profile, err := s.CreateProfile(active("claude", "same@example.com", "token", model.Metadata{
		model.MetadataOrganizationName: "Example Team",
	}))
	if err != nil {
		t.Fatal(err)
	}
	profileDir := s.ProfileDir("claude", profile.Number)
	manifestPath := filepath.Join(profileDir, "manifest.json")
	original, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profileDir, "config.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}

	_ = s.ListProfiles("claude")

	after, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, original) {
		t.Fatalf("manifest changed\nbefore=%s\nafter=%s", original, after)
	}
}

func TestProfilesByNumberCanBeAmbiguousAcrossAgents(t *testing.T) {
	s := New(t.TempDir())
	if _, err := s.CreateProfile(active("claude", "a@example.com", "fp-a", nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProfile(active("codex", "b@example.com", "fp-b", nil)); err != nil {
		t.Fatal(err)
	}

	matches := s.ProfilesByNumber(1, "")

	if len(matches) != 2 {
		t.Fatalf("matches len = %d", len(matches))
	}
	if matches[0].Agent != "claude" || matches[1].Agent != "codex" {
		t.Fatalf("matches = %+v", matches)
	}
}

func TestRemoveProfileDeletesProfileDirectory(t *testing.T) {
	home := t.TempDir()
	s := New(home)
	if _, err := s.CreateProfile(active("claude", "a@example.com", "fp-a", nil)); err != nil {
		t.Fatal(err)
	}

	if !s.RemoveProfile("claude", 1) {
		t.Fatal("expected first remove to return true")
	}
	if _, err := os.Stat(filepath.Join(home, ".agent-switch/profiles/claude/1")); !os.IsNotExist(err) {
		t.Fatalf("profile dir still exists err=%v", err)
	}
	if s.RemoveProfile("claude", 1) {
		t.Fatal("expected second remove to return false")
	}
}

func TestFindDuplicateKeepsProfilesWithoutReadableNameApart(t *testing.T) {
	s := New(t.TempDir())
	for _, fingerprint := range []string{"key-a", "key-b"} {
		if _, err := s.CreateProfile(active("codex", "unknown", fingerprint, nil)); err != nil {
			t.Fatal(err)
		}
	}
	next := active("codex", "unknown", "key-c", nil)

	if duplicate, ok := s.FindDuplicate(next); ok {
		t.Fatalf("unexpected duplicate = %+v", duplicate)
	}
	if removed := s.RemoveDuplicateProfiles(next, 1); len(removed) != 0 {
		t.Fatalf("removed = %+v", removed)
	}
	if profiles := s.ListProfiles("codex"); len(profiles) != 2 {
		t.Fatalf("profiles = %+v", profiles)
	}
}

func TestFindDuplicateTrustsExactCredentialsOverAnotherUsersIdentity(t *testing.T) {
	s := New(t.TempDir())
	browser := active("claude", "annapo@example.com", "browser-login", model.Metadata{
		model.MetadataAccountUUID:      "account-1",
		model.MetadataOrganizationUUID: "team-org",
	})
	if _, err := s.CreateProfile(browser); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProfile(active("claude", "bot", "token-login", nil)); err != nil {
		t.Fatal(err)
	}
	other := active("claude", "other@example.com", "other-login", model.Metadata{
		model.MetadataAccountUUID:      "account-2",
		model.MetadataOrganizationUUID: "other-org",
	})
	if _, err := s.CreateProfile(other); err != nil {
		t.Fatal(err)
	}

	for fingerprint, want := range map[string]int{"token-login": 2, "other-login": 3} {
		mismatched := browser
		mismatched.Fingerprint = fingerprint
		duplicate, ok := s.FindDuplicate(mismatched)
		if !ok || duplicate.Number != want {
			t.Fatalf("fingerprint %s duplicate = %+v ok=%v", fingerprint, duplicate, ok)
		}
	}
}

func TestListProfilesReadsCredentialKindFromSnapshot(t *testing.T) {
	home := t.TempDir()
	profileDir := filepath.Join(home, ".agent-switch/profiles/claude/1")
	writeStoreJSON(t, filepath.Join(profileDir, "manifest.json"), map[string]any{
		"agent":    "claude",
		"number":   1,
		"label":    "bot",
		"metadata": map[string]any{model.MetadataCredentialKind: "token"},
	})
	writeStoreJSON(t, filepath.Join(profileDir, "keychain.json"), map[string]any{
		"secret": `{"claudeAiOauth":{"accessToken":"access","refreshToken":"refresh"}}`,
	})
	s := New(home)

	if kind := s.ListProfiles("claude")[0].Metadata.String(model.MetadataCredentialKind); kind != "oauth" {
		t.Fatalf("kind = %q", kind)
	}
	writeStoreJSON(t, filepath.Join(profileDir, "keychain.json"), map[string]any{
		"secret": `{"claudeAiOauth":{"accessToken":"sk-ant-oat01-test","refreshToken":null,"scopes":["user:inference"]}}`,
	})
	if metadata := s.ListProfiles("claude")[0].Metadata; metadata.String(model.MetadataCredentialKind) != "token" || metadata.String(model.MetadataTokenID) == "" {
		t.Fatalf("token metadata = %#v", metadata)
	}
	if err := os.Remove(filepath.Join(profileDir, "keychain.json")); err != nil {
		t.Fatal(err)
	}
	if metadata := s.ListProfiles("claude")[0].Metadata; metadata.String(model.MetadataCredentialKind) != "" || metadata.String(model.MetadataTokenID) != "" {
		t.Fatalf("metadata without snapshot = %#v", metadata)
	}
}

func TestListProfilesReadsAgentAndNumberFromDirectory(t *testing.T) {
	home := t.TempDir()
	writeStoreJSON(t, filepath.Join(home, ".agent-switch/profiles/claude/3/manifest.json"), map[string]any{
		"agent":  "../../outside",
		"number": 9,
		"label":  "annapo@example.com",
	})
	s := New(home)

	profiles := s.ListProfiles("")

	if len(profiles) != 1 || profiles[0].Agent != "claude" || profiles[0].Number != 3 {
		t.Fatalf("profiles = %+v", profiles)
	}
	if !s.RemoveProfile(profiles[0].Agent, profiles[0].Number) {
		t.Fatal("profile was not removed")
	}
	if _, err := os.Stat(filepath.Join(home, ".agent-switch/profiles/claude/3")); !os.IsNotExist(err) {
		t.Fatalf("profile directory still exists: %v", err)
	}
}

func TestCreateProfileNeverReusesAnExistingDirectory(t *testing.T) {
	home := t.TempDir()
	taken := filepath.Join(home, ".agent-switch/profiles/claude/1")
	if err := os.MkdirAll(taken, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taken, "keychain.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := New(home)

	profile, err := s.CreateProfile(active("claude", "annapo@example.com", "fp", nil))

	if err != nil || profile.Number != 2 {
		t.Fatalf("profile = %+v err=%v", profile, err)
	}
}

func TestCreateProfileKeepsProfilesPrivate(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".agent-switch")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	s := New(home)

	if _, err := s.CreateProfile(active("claude", "annapo@example.com", "fp", nil)); err != nil {
		t.Fatal(err)
	}

	for _, dir := range []string{root, s.ProfileDir("claude", 1)} {
		info, err := os.Stat(dir)
		if err != nil || info.Mode().Perm() != 0o700 {
			t.Fatalf("%s mode = %v err=%v", dir, info.Mode().Perm(), err)
		}
	}
}

func TestListProfilesFingerprintsTheSavedAccountLogin(t *testing.T) {
	home := t.TempDir()
	profileDir := filepath.Join(home, ".agent-switch/profiles/claude/1")
	writeStoreJSON(t, filepath.Join(profileDir, "manifest.json"), map[string]any{
		"agent":       "claude",
		"number":      1,
		"label":       "bot",
		"fingerprint": "fingerprint-of-the-whole-item",
	})
	writeStoreJSON(t, filepath.Join(profileDir, "keychain.json"), map[string]any{
		"secret": `{"claudeAiOauth":{"accessToken":"sk-ant-oat01-test","refreshToken":null,"scopes":["user:inference"]},"mcpOAuth":{"notion":{"refreshToken":"old"}}}`,
	})
	current := active("claude", "bot", identity.ClaudeKeychainFingerprint(`{"claudeAiOauth":{"accessToken":"sk-ant-oat01-test","refreshToken":null,"scopes":["user:inference"]},"mcpOAuth":{"notion":{"refreshToken":"new"}}}`), nil)

	holder, ok := New(home).CredentialsHolder(current)
	if !ok || holder.Number != 1 {
		t.Fatalf("holder = %+v ok=%v", holder, ok)
	}
}

func TestListProfilesRemovesMCPLoginsFromOldSnapshots(t *testing.T) {
	home := t.TempDir()
	profileDir := filepath.Join(home, ".agent-switch/profiles/claude/1")
	writeStoreJSON(t, filepath.Join(profileDir, "manifest.json"), map[string]any{"agent": "claude", "number": 1, "label": "bot"})
	writeStoreJSON(t, filepath.Join(profileDir, "keychain.json"), map[string]any{
		"service": "Claude Code-credentials",
		"account": "annapo",
		"secret":  `{"claudeAiOauth":{"accessToken":"sk-ant-oat01-test","refreshToken":null,"scopes":["user:inference"]},"mcpOAuth":{"notion":{"refreshToken":"old"}}}`,
	})

	New(home).ListProfiles("claude")

	path := filepath.Join(profileDir, "keychain.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot map[string]string
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(snapshot["secret"], "mcpOAuth") || !strings.Contains(snapshot["secret"], "sk-ant-oat01-test") || snapshot["account"] != "annapo" || snapshot["service"] != "Claude Code-credentials" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("snapshot info=%v err=%v", info, err)
	}
}
