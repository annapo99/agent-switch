package provider

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/annapo99/agent-switch/internal/model"
)

type fakeKeychain struct {
	item   *KeychainItem
	writes []KeychainItem
}

func (f *fakeKeychain) IsAvailable(home string) bool { return true }
func (f *fakeKeychain) ReadItem(service string) (*KeychainItem, error) {
	return f.item, nil
}
func (f *fakeKeychain) WriteItem(service, account, secret string) error {
	f.item = &KeychainItem{Account: account, Secret: secret}
	f.writes = append(f.writes, *f.item)
	return nil
}

func writeJSON(t *testing.T, home, rel string, payload any) {
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

func readJSONFile(t *testing.T, home, rel string) map[string]any {
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

func unsignedJWT(payload map[string]any) string {
	data, _ := json.Marshal(payload)
	return "header." + strings.TrimRight(base64.RawURLEncoding.EncodeToString(data), "=") + ".signature"
}

func TestClaudeProviderDetectsFileAccountLabelAndFingerprint(t *testing.T) {
	home := t.TempDir()
	writeJSON(t, home, ".claude/.credentials.json", map[string]any{
		"email":       "annapo@example.com",
		"accessToken": "test-token-1",
	})
	p := NewClaudeProvider(nil)

	first, ok := p.Detect(home)
	second, secondOK := p.Detect(home)

	if !ok || !secondOK {
		t.Fatal("expected claude account")
	}
	if first.Agent != "claude" || first.DisplayName != "Claude" || first.Label != "annapo@example.com" {
		t.Fatalf("first = %+v", first)
	}
	if first.Source != "~/.claude/.credentials.json" {
		t.Fatalf("source = %q", first.Source)
	}
	if first.Fingerprint == "" || first.Fingerprint != second.Fingerprint {
		t.Fatalf("fingerprints = %q %q", first.Fingerprint, second.Fingerprint)
	}
}

func TestClaudeProviderPrefersKeychainAndUsesClaudeJSONLabel(t *testing.T) {
	home := t.TempDir()
	writeJSON(t, home, ".claude/.credentials.json", map[string]any{
		"email":       "file@example.com",
		"accessToken": "file-test-token",
	})
	writeJSON(t, home, ".claude.json", map[string]any{
		"oauthAccount": map[string]any{"emailAddress": "annapo@example.com"},
	})
	backend := &fakeKeychain{item: &KeychainItem{Account: "annapo", Secret: `{"claudeAiOauth":{"accessToken":"keychain-access"}}`}}
	p := NewClaudeProvider(backend)

	account, ok := p.Detect(home)

	if !ok {
		t.Fatal("expected account")
	}
	if account.Label != "annapo@example.com" {
		t.Fatalf("label = %q", account.Label)
	}
	if account.Source != "Keychain: Claude Code-credentials" {
		t.Fatalf("source = %q", account.Source)
	}
	if len(account.AuthFiles) != 1 || account.AuthFiles[0] != "keychain:Claude Code-credentials" {
		t.Fatalf("auth files = %#v", account.AuthFiles)
	}
}

func TestClaudeProviderIncludesClaudeJSONMetadata(t *testing.T) {
	home := t.TempDir()
	writeJSON(t, home, ".claude/.credentials.json", map[string]any{
		"email":       "annapo@example.com",
		"accessToken": "file-test-token",
	})
	writeJSON(t, home, ".claude.json", map[string]any{
		"oauthAccount": map[string]any{
			"organizationName":          "Example Team",
			"userRateLimitTier":         "default_claude_max_5x",
			"organizationRateLimitTier": "default_raven",
			"seatTier":                  "team_tier_1",
		},
	})
	p := NewClaudeProvider(nil)

	account, ok := p.Detect(home)

	if !ok {
		t.Fatal("expected account")
	}
	if got := account.Metadata.String("organization_name"); got != "Example Team" {
		t.Fatalf("organization = %q", got)
	}
	if got := account.Metadata.String("user_rate_limit_tier"); got != "default_claude_max_5x" {
		t.Fatalf("user tier = %q", got)
	}
}

func TestKeychainSnapshotAndApplyRoundTripsSecret(t *testing.T) {
	home := t.TempDir()
	original := `{"claudeAiOauth":{"accessToken":"original-access","refreshToken":"original-refresh"}}`
	backend := &fakeKeychain{item: &KeychainItem{Account: "annapo", Secret: original}}
	p := NewClaudeProvider(backend)
	account, ok := p.Detect(home)
	if !ok {
		t.Fatal("expected account")
	}
	profileDir := filepath.Join(home, ".agent-switch/profiles/claude/1")

	if err := p.SaveSnapshot(home, account, profileDir); err != nil {
		t.Fatal(err)
	}
	backend.item = &KeychainItem{Account: "other", Secret: `{"claudeAiOauth":{"accessToken":"other"}}`}
	if err := p.ApplySnapshot(home, profileDir); err != nil {
		t.Fatal(err)
	}

	if backend.item.Account != "annapo" || backend.item.Secret != original {
		t.Fatalf("restored item = %+v", backend.item)
	}
	if len(backend.writes) != 1 || backend.writes[0].Secret != original {
		t.Fatalf("writes = %+v", backend.writes)
	}
}

func TestCodexProviderDetectsJWTEmailAndAccountIDFallback(t *testing.T) {
	home := t.TempDir()
	writeJSON(t, home, ".codex/auth.json", map[string]any{
		"tokens": map[string]any{"id_token": unsignedJWT(map[string]any{"email": "annapo.codex@example.com"})},
	})
	p := NewCodexProvider()

	account, ok := p.Detect(home)
	if !ok || account.Label != "annapo.codex@example.com" || account.Source != "~/.codex/auth.json" {
		t.Fatalf("account = %+v ok=%v", account, ok)
	}

	home2 := t.TempDir()
	writeJSON(t, home2, ".codex/auth.json", map[string]any{
		"tokens": map[string]any{"account_id": "acct_123", "access_token": "test-token-2"},
	})
	account, ok = p.Detect(home2)
	if !ok || account.Label != "acct_123" {
		t.Fatalf("fallback account = %+v ok=%v", account, ok)
	}
}

func TestFileSnapshotAndApplyRoundTripsAuthFile(t *testing.T) {
	home := t.TempDir()
	writeJSON(t, home, ".claude/.credentials.json", map[string]any{
		"email":       "annapo@example.com",
		"accessToken": "test-token-1",
	})
	p := NewClaudeProvider(nil)
	account, ok := p.Detect(home)
	if !ok {
		t.Fatal("expected account")
	}
	profileDir := filepath.Join(home, ".agent-switch/profiles/claude/1")

	if err := p.SaveSnapshot(home, account, profileDir); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, home, ".claude/.credentials.json", map[string]any{
		"email":       "other@example.com",
		"accessToken": "test-token-2",
	})
	if err := p.ApplySnapshot(home, profileDir); err != nil {
		t.Fatal(err)
	}

	restored := readJSONFile(t, home, ".claude/.credentials.json")
	if restored["email"] != "annapo@example.com" || restored["accessToken"] != "test-token-1" {
		t.Fatalf("restored = %+v", restored)
	}
}

var _ Provider = NewCodexProvider()
var _ Provider = NewClaudeProvider(&fakeKeychain{})
var _ = model.ActiveAccount{}
