package usage

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/annapo99/agent-switch/internal/model"
)

func unsignedJWT(payload map[string]any) string {
	data, _ := json.Marshal(payload)
	return "header." + strings.TrimRight(base64.RawURLEncoding.EncodeToString(data), "=") + ".signature"
}

func writeJSON(t *testing.T, home, rel string, payload any) {
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

func useSeoulTime(t *testing.T) {
	t.Helper()
	previous := time.Local
	time.Local = time.FixedZone("KST", 9*60*60)
	t.Cleanup(func() {
		time.Local = previous
	})
}

func TestCswapPayloadMapsUsageWindowsByEmail(t *testing.T) {
	metadata := MetadataByEmailFromCswapPayload(map[string]any{
		"accounts": []any{
			map[string]any{
				"email":             "annapo.claude@example.com",
				"organizationName":  "Example Team",
				"usageAgeSeconds":   float64(3660),
				"usageStatus":       "ok",
				"irrelevantIgnored": true,
				"usage": map[string]any{
					"fiveHour": map[string]any{"pct": float64(90), "clock": "11:29", "countdown": "1m"},
					"sevenDay": map[string]any{"pct": float64(26), "clock": "Jul 10 09:59", "countdown": "1d 22h"},
					"scoped": []any{
						map[string]any{"name": "Fable", "pct": float64(41), "clock": "Jul 10 09:59", "countdown": "1d 22h"},
					},
				},
			},
		},
	})

	key := model.MetadataKey{Agent: "claude", Label: "annapo.claude@example.com", Organization: "name:example team"}
	got := metadata[key]

	if got.String("organization_name") != "Example Team" {
		t.Fatalf("metadata = %#v", got)
	}
	rows := got["usage_limits"].([]any)
	if len(rows) != 3 {
		t.Fatalf("rows len = %d", len(rows))
	}
	last := rows[2].(map[string]any)
	if last["label"] != "Fable" || last["age"] != "1h ago" {
		t.Fatalf("last row = %#v", last)
	}
}

func TestTokenStatusTextMapsOAuthRowByEmail(t *testing.T) {
	statuses := TokenStatusByEmailFromCswapText(`Accounts:
  3: annapo.claude@example.com [Example Team]
     ├ 5h:     90%   resets 11:29         in 1m
     └ Fable:  41%   resets Jul 10 09:59  in 1d 22h
     • oauth: fresh, refresh token yes, expires 14:36 in 3h 7m
`)

	key := model.MetadataKey{Agent: "claude", Label: "annapo.claude@example.com"}
	if got := statuses[key].String("oauth_status"); got != "oauth: fresh, refresh token yes, expires 14:36 in 3h 7m" {
		t.Fatalf("oauth = %q", got)
	}
}

func TestClaudeOAuthProviderLoadsSavedSnapshotUsageAndOAuth(t *testing.T) {
	useSeoulTime(t)

	home := t.TempDir()
	writeJSON(t, home, ".agent-switch/profiles/claude/1/manifest.json", map[string]any{
		"agent":        "claude",
		"display_name": "Claude",
		"number":       1,
		"label":        "annapo.claude@example.com",
		"metadata": map[string]any{
			model.MetadataOrganizationUUID: "team-org",
		},
	})
	writeJSON(t, home, ".agent-switch/profiles/claude/1/keychain.json", map[string]any{
		"service": "Claude Code-credentials",
		"account": "annapo",
		"secret":  `{"claudeAiOauth":{"accessToken":"claude-access-token","refreshToken":"claude-refresh-token","expiresAt":1783504032000}}`,
	})
	provider := ClaudeOAuthProvider{
		Fetcher: func(token string) (map[string]any, error) {
			if token != "claude-access-token" {
				t.Fatalf("token = %q", token)
			}
			return map[string]any{
				"five_hour": map[string]any{
					"utilization": float64(48),
					"resets_at":   "2026-07-08T09:47:12Z",
				},
				"seven_day": map[string]any{
					"utilization": float64(27),
					"resets_at":   "2026-07-12T17:59:00Z",
				},
				"limits": []any{
					map[string]any{
						"percent":   float64(15),
						"resets_at": "2026-07-12T17:59:00Z",
						"scope": map[string]any{
							"model": map[string]any{"display_name": "Fable"},
						},
					},
				},
			}, nil
		},
		RestrictToCurrentHome: false,
		NowEpoch:              1783486262,
	}

	metadata := provider.Load(home)

	key := model.MetadataKey{Agent: "claude", Label: "annapo.claude@example.com", Organization: "uuid:team-org"}
	got := metadata[key]
	rows, ok := got["usage_limits"].([]any)
	if !ok {
		t.Fatalf("metadata = %#v", metadata)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %#v", rows)
	}
	if rows[0].(map[string]any)["reset_at"] != "18:47" || rows[0].(map[string]any)["remaining"] != "in 4h 56m" {
		t.Fatalf("first row = %#v", rows[0])
	}
	if rows[2].(map[string]any)["label"] != "Fable" {
		t.Fatalf("scoped row = %#v", rows[2])
	}
	if got.String("oauth_status") != "oauth: fresh, refresh token yes, expires 18:47 in 4h 56m" {
		t.Fatalf("oauth = %#v", got)
	}
}

func TestClaudeOAuthProviderRefreshesExpiredSnapshotBeforeUsageFetch(t *testing.T) {
	useSeoulTime(t)

	home := t.TempDir()
	writeJSON(t, home, ".agent-switch/profiles/claude/1/manifest.json", map[string]any{
		"agent":        "claude",
		"display_name": "Claude",
		"number":       1,
		"label":        "annapo.claude@example.com",
	})
	writeJSON(t, home, ".agent-switch/profiles/claude/1/keychain.json", map[string]any{
		"service": "Claude Code-credentials",
		"account": "annapo",
		"secret":  `{"claudeAiOauth":{"accessToken":"expired-token","refreshToken":"claude-refresh-token","expiresAt":1783480000000}}`,
	})
	provider := ClaudeOAuthProvider{
		Fetcher: func(token string) (map[string]any, error) {
			if token != "fresh-token" {
				t.Fatalf("token = %q", token)
			}
			return map[string]any{
				"five_hour": map[string]any{"utilization": float64(12)},
			}, nil
		},
		TokenRefresher: func(credentials string) (string, error) {
			if !strings.Contains(credentials, "expired-token") {
				t.Fatalf("credentials = %q", credentials)
			}
			return `{"claudeAiOauth":{"accessToken":"fresh-token","refreshToken":"fresh-refresh-token","expiresAt":1783504032000}}`, nil
		},
		RestrictToCurrentHome: false,
		NowEpoch:              1783486262,
	}

	metadata := provider.Load(home)

	key := model.MetadataKey{Agent: "claude", Label: "annapo.claude@example.com"}
	rows := metadata[key]["usage_limits"].([]any)
	if rows[0].(map[string]any)["used_percentage"] != float64(12) {
		t.Fatalf("rows = %#v", rows)
	}
	snapshot := readJSON(filepath.Join(home, ".agent-switch/profiles/claude/1/keychain.json"))
	if !strings.Contains(snapshot["secret"].(string), "fresh-token") {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestClaudeOAuthProviderMarksInvalidRefreshToken(t *testing.T) {
	useSeoulTime(t)

	home := t.TempDir()
	writeJSON(t, home, ".agent-switch/profiles/claude/1/manifest.json", map[string]any{
		"agent":        "claude",
		"display_name": "Claude",
		"number":       1,
		"label":        "annapo.claude@example.com",
	})
	writeJSON(t, home, ".agent-switch/profiles/claude/1/keychain.json", map[string]any{
		"service": "Claude Code-credentials",
		"account": "annapo",
		"secret":  `{"claudeAiOauth":{"accessToken":"expired-token","refreshToken":"dead-refresh-token","expiresAt":1783480000000}}`,
	})
	provider := ClaudeOAuthProvider{
		Fetcher: func(token string) (map[string]any, error) {
			t.Fatalf("fetch should not run after invalid refresh")
			return nil, nil
		},
		TokenRefresher: func(credentials string) (string, error) {
			return "", errInvalidGrant
		},
		RestrictToCurrentHome: false,
		NowEpoch:              1783486262,
	}

	metadata := provider.Load(home)

	key := model.MetadataKey{Agent: "claude", Label: "annapo.claude@example.com"}
	if got := metadata[key].String("oauth_status"); !strings.Contains(got, "refresh token invalid") {
		t.Fatalf("oauth = %q", got)
	}
}

func TestCodexPayloadMapsUsagePlanAndOAuth(t *testing.T) {
	useSeoulTime(t)

	metadata := MetadataByEmailFromCodexPayload(map[string]any{
		"email":     "annapo.codex@example.com",
		"plan_type": "pro",
		"rate_limit": map[string]any{
			"primary_window":   map[string]any{"limit_window_seconds": float64(18000), "reset_after_seconds": float64(17770), "reset_at": float64(1783504032), "used_percent": float64(0)},
			"secondary_window": map[string]any{"limit_window_seconds": float64(604800), "reset_after_seconds": float64(511763), "reset_at": float64(1783998024), "used_percent": float64(8)},
		},
		"additional_rate_limits": []any{
			map[string]any{
				"limit_name": "GPT-5.3-Codex-Spark",
				"rate_limit": map[string]any{
					"primary_window":   map[string]any{"limit_window_seconds": float64(18000), "reset_after_seconds": float64(18000), "reset_at": float64(1783504262), "used_percent": float64(0)},
					"secondary_window": map[string]any{"limit_window_seconds": float64(604800), "reset_after_seconds": float64(604800), "reset_at": float64(1784091062), "used_percent": float64(0)},
				},
			},
		},
	}, "fallback@example.com", 1783486262)

	key := model.MetadataKey{Agent: "codex", Label: "annapo.codex@example.com"}
	rows := metadata[key]["usage_limits"].([]any)
	if len(rows) != 4 {
		t.Fatalf("rows = %#v", rows)
	}
	if rows[0].(map[string]any)["reset_at"] != "18:47" {
		t.Fatalf("first row = %#v", rows[0])
	}
	if rows[2].(map[string]any)["label"] != "Spark 5h" {
		t.Fatalf("spark row = %#v", rows[2])
	}
	if metadata[key]["status_lines"].([]any)[0] != "plan: pro" {
		t.Fatalf("status = %#v", metadata[key]["status_lines"])
	}
}

func TestOAuthStatusFromCodexAuthUsesAccessExpiryAndRefreshPresence(t *testing.T) {
	useSeoulTime(t)

	status := OAuthStatusFromCodexAuth(map[string]any{
		"tokens": map[string]any{
			"access_token":  unsignedJWT(map[string]any{"exp": float64(1783504032)}),
			"refresh_token": "test-refresh-token",
		},
	}, 1783486262)

	if status != "oauth: fresh, refresh token yes, expires 18:47 in 4h 56m" {
		t.Fatalf("status = %q", status)
	}
}

func TestCodexUsageProviderAddsOAuthStatusFromAuth(t *testing.T) {
	useSeoulTime(t)

	home := t.TempDir()
	writeJSON(t, home, ".codex/auth.json", map[string]any{
		"tokens": map[string]any{
			"access_token": unsignedJWT(map[string]any{
				"email": "annapo.codex@example.com",
				"exp":   float64(1783504032),
			}),
			"refresh_token": "test-refresh-token",
		},
	})
	provider := CodexProvider{
		Fetcher: func(token string) (map[string]any, error) {
			return map[string]any{"email": "annapo.codex@example.com", "plan_type": "pro"}, nil
		},
		RestrictToCurrentHome: false,
		NowEpoch:              1783486262,
	}

	metadata := provider.Load(home)

	key := model.MetadataKey{Agent: "codex", Label: "annapo.codex@example.com"}
	if metadata[key].String("oauth_status") != "oauth: fresh, refresh token yes, expires 18:47 in 4h 56m" {
		t.Fatalf("metadata = %#v", metadata[key])
	}
	if metadata[key]["status_lines"].([]any)[0] != "plan: pro" {
		t.Fatalf("metadata = %#v", metadata[key])
	}
}

func TestMergeProfilesWithUsageMetadataDoesNotMutateOriginal(t *testing.T) {
	profile := model.Profile{Agent: "claude", Label: "annapo.claude@example.com", Metadata: model.Metadata{"organization_name": "Stored Org"}}
	codex := model.Profile{Agent: "codex", Label: "annapo.claude@example.com", Metadata: model.Metadata{}}

	merged := MergeProfilesWithUsageMetadata([]model.Profile{profile, codex}, map[model.MetadataKey]model.Metadata{
		{Agent: "claude", Label: "annapo.claude@example.com"}: {
			"organization_name": "Example Team",
			"oauth_status":      "oauth: fresh",
		},
	})

	if merged[0].Metadata.String("organization_name") != "Example Team" {
		t.Fatalf("merged = %#v", merged[0].Metadata)
	}
	if profile.Metadata.String("organization_name") != "Stored Org" {
		t.Fatalf("original mutated = %#v", profile.Metadata)
	}
	if len(merged[1].Metadata) != 0 {
		t.Fatalf("codex mutated = %#v", merged[1].Metadata)
	}
}

func TestMergeProfilesWithUsageMetadataSeparatesClaudeOrganizations(t *testing.T) {
	profiles := []model.Profile{
		{Agent: "claude", Label: "same@example.com", Metadata: model.Metadata{
			model.MetadataOrganizationUUID: "team-org",
		}},
		{Agent: "claude", Label: "same@example.com", Metadata: model.Metadata{
			model.MetadataOrganizationUUID: "enterprise-org",
		}},
	}
	metadata := map[model.MetadataKey]model.Metadata{
		{Agent: "claude", Label: "same@example.com", Organization: "uuid:team-org"}: {
			"oauth_status": "oauth: team",
		},
		{Agent: "claude", Label: "same@example.com", Organization: "uuid:enterprise-org"}: {
			"oauth_status": "oauth: enterprise",
		},
		{Agent: "claude", Label: "same@example.com"}: {
			"oauth_status": "oauth: ambiguous fallback",
		},
	}

	merged := MergeProfilesWithUsageMetadata(profiles, metadata)

	if merged[0].Metadata.String("oauth_status") != "oauth: team" ||
		merged[1].Metadata.String("oauth_status") != "oauth: enterprise" {
		t.Fatalf("merged = %+v", merged)
	}
}

func writeUsageProfile(t *testing.T, home, number, label, accountUUID string) {
	t.Helper()
	writeJSON(t, home, ".agent-switch/profiles/claude/"+number+"/manifest.json", map[string]any{
		"agent": "claude", "number": number, "label": label,
		"metadata": map[string]any{model.MetadataAccountUUID: accountUUID, model.MetadataOrganizationUUID: "team-org"},
	})
	writeJSON(t, home, ".agent-switch/profiles/claude/"+number+"/keychain.json", map[string]any{
		"service": "Claude Code-credentials",
		"secret":  `{"claudeAiOauth":{"accessToken":"access-` + number + `","refreshToken":"refresh","expiresAt":1783504032000}}`,
	})
}

func usagePayload(fiveHour float64) map[string]any {
	return map[string]any{
		"five_hour": map[string]any{"utilization": fiveHour, "resets_at": "2026-07-08T09:47:12Z"},
		"seven_day": map[string]any{"utilization": float64(27), "resets_at": "2026-07-12T17:59:00Z"},
	}
}

func usageRows(t *testing.T, metadata map[model.MetadataKey]model.Metadata, label string) []any {
	t.Helper()
	rows, _ := metadata[model.MetadataKey{Agent: "claude", Label: label, Organization: "uuid:team-org"}]["usage_limits"].([]any)
	return rows
}

func TestClaudeOAuthProviderShowsLastUsageWhenFetchIsRateLimited(t *testing.T) {
	useSeoulTime(t)
	home := t.TempDir()
	writeUsageProfile(t, home, "1", "annapo@example.com", "account-annapo")
	fresh := ClaudeOAuthProvider{NowEpoch: 1783486262, Fetcher: func(string) (map[string]any, error) { return usagePayload(48), nil }}
	fresh.Load(home)

	limited := ClaudeOAuthProvider{NowEpoch: 1783486262 + 600, Fetcher: func(string) (map[string]any, error) { return nil, errHTTPStatus(429) }}
	rows := usageRows(t, limited.Load(home), "annapo@example.com")

	if len(rows) != 2 || rows[0].(map[string]any)["used_percentage"] != float64(48) || rows[1].(map[string]any)["age"] != "10m ago" {
		t.Fatalf("rows = %#v", rows)
	}
}

func TestClaudeOAuthProviderUsesClaudeCodeUsageCacheForTheSameAccount(t *testing.T) {
	useSeoulTime(t)
	home := t.TempDir()
	writeUsageProfile(t, home, "1", "annapo@example.com", "account-annapo")
	writeUsageProfile(t, home, "2", "other@example.com", "account-other")
	writeJSON(t, home, ".claude.json", map[string]any{"cachedUsageUtilization": map[string]any{
		"accountUuid": "account-annapo",
		"fetchedAtMs": (1783486262 - 300) * 1000,
		"utilization": usagePayload(7),
	}})
	limited := ClaudeOAuthProvider{NowEpoch: 1783486262, Fetcher: func(string) (map[string]any, error) { return nil, errHTTPStatus(429) }}

	metadata := limited.Load(home)

	rows := usageRows(t, metadata, "annapo@example.com")
	if len(rows) != 2 || rows[0].(map[string]any)["used_percentage"] != float64(7) || rows[1].(map[string]any)["age"] != "5m ago" {
		t.Fatalf("annapo rows = %#v", rows)
	}
	if other := usageRows(t, metadata, "other@example.com"); len(other) != 0 {
		t.Fatalf("other account got annapo usage: %#v", other)
	}
}

func writeTokenUsageProfile(t *testing.T, home string) {
	t.Helper()
	writeJSON(t, home, ".agent-switch/profiles/claude/2/manifest.json", map[string]any{"agent": "claude", "number": 2, "label": "bot"})
	writeJSON(t, home, ".agent-switch/profiles/claude/2/keychain.json", map[string]any{
		"service": "Claude Code-credentials",
		"secret":  `{"claudeAiOauth":{"accessToken":"bot-token","refreshToken":null,"expiresAt":1815022262000,"scopes":["user:inference"]}}`,
	})
}

func tokenRows(metadata map[model.MetadataKey]model.Metadata) []any {
	rows, _ := metadata[model.MetadataKey{Agent: "claude", Label: "bot"}]["usage_limits"].([]any)
	return rows
}

func TestClaudeOAuthProviderReadsTokenProfileUsageFromRateLimitHeaders(t *testing.T) {
	useSeoulTime(t)
	home := t.TempDir()
	writeTokenUsageProfile(t, home)
	probed := 0
	provider := ClaudeOAuthProvider{
		NowEpoch: 1783486262,
		Fetcher: func(string) (map[string]any, error) {
			t.Fatal("usage endpoint called for a token profile")
			return nil, nil
		},
		Prober: func(token string) (map[string]any, error) {
			probed++
			if token != "bot-token" {
				t.Fatalf("token = %q", token)
			}
			return usagePayload(45), nil
		},
	}

	rows := tokenRows(provider.Load(home))
	rows = append(rows, tokenRows(ClaudeOAuthProvider{NowEpoch: 1783486262 + 120, Prober: provider.Prober}.Load(home))...)

	if probed != 1 || len(rows) != 4 || rows[0].(map[string]any)["used_percentage"] != float64(45) || rows[3].(map[string]any)["age"] != "2m ago" {
		t.Fatalf("probed=%d rows=%#v", probed, rows)
	}
}

func TestClaudeOAuthProviderProbesTokenProfileAgainAfterFiveMinutes(t *testing.T) {
	useSeoulTime(t)
	home := t.TempDir()
	writeTokenUsageProfile(t, home)
	ClaudeOAuthProvider{NowEpoch: 1783486262, Prober: func(string) (map[string]any, error) { return usagePayload(45), nil }}.Load(home)

	rows := tokenRows(ClaudeOAuthProvider{NowEpoch: 1783486262 + 301, Prober: func(string) (map[string]any, error) { return usagePayload(50), nil }}.Load(home))

	if len(rows) != 2 || rows[0].(map[string]any)["used_percentage"] != float64(50) {
		t.Fatalf("rows = %#v", rows)
	}
}

func TestUsagePayloadFromRateLimitHeaders(t *testing.T) {
	header := http.Header{}
	header.Set("anthropic-ratelimit-unified-5h-utilization", "0.45")
	header.Set("anthropic-ratelimit-unified-5h-reset", "1783490862")
	header.Set("anthropic-ratelimit-unified-7d-utilization", "0.48")
	header.Set("anthropic-ratelimit-unified-7d-reset", "1783879140")

	payload := usagePayloadFromRateLimitHeaders(header)

	fiveHour := payload["five_hour"].(map[string]any)
	sevenDay := payload["seven_day"].(map[string]any)
	if fiveHour["utilization"] != float64(45) || fiveHour["resets_at"] != "2026-07-08T06:07:42Z" || sevenDay["utilization"] != float64(48) {
		t.Fatalf("payload = %#v", payload)
	}
	if empty := usagePayloadFromRateLimitHeaders(http.Header{}); len(empty) != 0 {
		t.Fatalf("payload without headers = %#v", empty)
	}
}

func TestClaudeOAuthProviderLeavesTheActiveLoginToClaudeCode(t *testing.T) {
	useSeoulTime(t)

	home := t.TempDir()
	active := `{"claudeAiOauth":{"accessToken":"expired-token","refreshToken":"active-refresh-token","expiresAt":1783480000000}}`
	for number, secret := range map[string]string{
		"1": active,
		"2": `{"claudeAiOauth":{"accessToken":"expired-token","refreshToken":"saved-refresh-token","expiresAt":1783480000000}}`,
	} {
		writeJSON(t, home, ".agent-switch/profiles/claude/"+number+"/manifest.json", map[string]any{
			"agent": "claude", "number": number, "label": "account" + number + "@example.com",
		})
		writeJSON(t, home, ".agent-switch/profiles/claude/"+number+"/keychain.json", map[string]any{
			"service": "Claude Code-credentials", "account": "annapo", "secret": secret,
		})
	}
	var refreshed []string
	provider := ClaudeOAuthProvider{
		Fetcher: func(token string) (map[string]any, error) { return nil, http.ErrServerClosed },
		TokenRefresher: func(credentials string) (string, error) {
			refreshed = append(refreshed, credentials)
			return `{"claudeAiOauth":{"accessToken":"fresh-token","refreshToken":"fresh-refresh-token","expiresAt":1783504032000}}`, nil
		},
		ActiveCredentials: func(string) (string, bool) {
			return strings.TrimSuffix(active, "}") + `,"mcpOAuth":{"server":{"accessToken":"mcp"}}}`, true
		},
		NowEpoch: 1783486262,
	}

	provider.Load(home)

	if len(refreshed) != 1 || !strings.Contains(refreshed[0], "saved-refresh-token") {
		t.Fatalf("refreshed = %#v", refreshed)
	}
	snapshot := readJSON(filepath.Join(home, ".agent-switch/profiles/claude/1/keychain.json"))
	if snapshot["secret"] != active {
		t.Fatalf("active login was refreshed behind Claude Code: %#v", snapshot)
	}
}

func TestClaudeOAuthProviderRefreshesNothingItCannotTellApartFromTheActiveLogin(t *testing.T) {
	useSeoulTime(t)

	expired := func(refreshToken string) string {
		return `{"claudeAiOauth":{"accessToken":"expired-token","refreshToken":"` + refreshToken + `","expiresAt":1783480000000}}`
	}
	saveProfiles := func(home string) {
		for number, organization := range map[string]string{"1": "team-org", "2": "enterprise-org"} {
			writeJSON(t, home, ".agent-switch/profiles/claude/"+number+"/manifest.json", map[string]any{
				"agent": "claude", "label": "same@example.com",
				"metadata": map[string]any{"account_uuid": "account-1", "organization_uuid": organization},
			})
			writeJSON(t, home, ".agent-switch/profiles/claude/"+number+"/keychain.json", map[string]any{
				"service": "Claude Code-credentials", "account": "annapo", "secret": expired("saved-" + organization),
			})
		}
	}
	load := func(home string, active func(string) (string, bool)) []string {
		var refreshed []string
		ClaudeOAuthProvider{
			Fetcher: func(token string) (map[string]any, error) { return nil, http.ErrServerClosed },
			TokenRefresher: func(credentials string) (string, error) {
				refreshed = append(refreshed, credentials)
				return expired("fresh"), nil
			},
			ActiveCredentials: active,
			NowEpoch:          1783486262,
		}.Load(home)
		return refreshed
	}

	unreadable := t.TempDir()
	saveProfiles(unreadable)
	if refreshed := load(unreadable, func(string) (string, bool) { return "", false }); len(refreshed) != 0 {
		t.Fatalf("refreshed without knowing the active login: %#v", refreshed)
	}

	sameIdentity := t.TempDir()
	saveProfiles(sameIdentity)
	writeJSON(t, sameIdentity, ".claude.json", map[string]any{"oauthAccount": map[string]any{
		"accountUuid": "Account-1", "organizationUuid": "team-org",
	}})
	refreshed := load(sameIdentity, func(string) (string, bool) { return expired("rotated-by-claude-code"), true })
	if len(refreshed) != 1 || !strings.Contains(refreshed[0], "saved-enterprise-org") {
		t.Fatalf("refreshed = %#v", refreshed)
	}
}
