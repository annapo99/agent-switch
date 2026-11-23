package identity

import (
	"strings"
	"testing"

	"github.com/annapo99/agent-switch/internal/model"
)

func TestSameLogicalAccount(t *testing.T) {
	tests := []struct {
		name    string
		profile model.Profile
		account model.ActiveAccount
		want    bool
	}{
		{
			name: "same email different organization uuid",
			profile: model.Profile{Agent: "claude", Label: "same@example.com", Metadata: model.Metadata{
				model.MetadataAccountUUID: "account-1", model.MetadataOrganizationUUID: "team-org",
			}},
			account: model.ActiveAccount{Agent: "claude", Label: "same@example.com", Metadata: model.Metadata{
				model.MetadataAccountUUID: "account-1", model.MetadataOrganizationUUID: "enterprise-org",
			}},
			want: false,
		},
		{
			name: "same account and organization uuid",
			profile: model.Profile{Agent: "claude", Label: "old@example.com", Metadata: model.Metadata{
				model.MetadataAccountUUID: "account-1", model.MetadataOrganizationUUID: "team-org",
			}},
			account: model.ActiveAccount{Agent: "claude", Label: "new@example.com", Metadata: model.Metadata{
				model.MetadataAccountUUID: "account-1", model.MetadataOrganizationUUID: "team-org",
			}},
			want: true,
		},
		{
			name: "legacy organization names differ",
			profile: model.Profile{Agent: "claude", Label: "same@example.com", Metadata: model.Metadata{
				model.MetadataOrganizationName: "Example Team",
			}},
			account: model.ActiveAccount{Agent: "claude", Label: "same@example.com", Metadata: model.Metadata{
				model.MetadataOrganizationName: "Example Enterprise",
			}},
			want: false,
		},
		{
			name: "legacy organization names match",
			profile: model.Profile{Agent: "claude", Label: "SAME@example.com", Metadata: model.Metadata{
				model.MetadataOrganizationName: "Example Team",
			}},
			account: model.ActiveAccount{Agent: "claude", Label: "same@example.com", Metadata: model.Metadata{
				model.MetadataOrganizationName: "example team",
			}},
			want: true,
		},
		{
			name:    "codex keeps label fallback",
			profile: model.Profile{Agent: "codex", Label: "SAME@example.com"},
			account: model.ActiveAccount{Agent: "codex", Label: "same@example.com"},
			want:    true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := SameLogicalAccount(test.profile, test.account); got != test.want {
				t.Fatalf("SameLogicalAccount() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestOrganizationKeysOrdersNameBeforeUUID(t *testing.T) {
	keys := OrganizationKeys("claude", "Same@Example.com", model.Metadata{
		model.MetadataOrganizationName: "Example Team",
		model.MetadataOrganizationUUID: "TEAM-ORG",
	})

	if len(keys) != 2 ||
		keys[0] != (model.MetadataKey{Agent: "claude", Label: "same@example.com", Organization: "name:example team"}) ||
		keys[1] != (model.MetadataKey{Agent: "claude", Label: "same@example.com", Organization: "uuid:team-org"}) {
		t.Fatalf("keys = %#v", keys)
	}
}

func TestSameLogicalAccountIgnoresUnknownLabels(t *testing.T) {
	for _, agent := range []string{"codex", "claude"} {
		profile := model.Profile{Agent: agent, Label: "unknown"}
		account := model.ActiveAccount{Agent: agent, Label: "Unknown"}
		if SameLogicalAccount(profile, account) {
			t.Fatalf("%s profiles without a readable name matched each other", agent)
		}
	}
}

func TestSameLogicalAccountIgnoresUnknownLabelsWithinAnOrganizationName(t *testing.T) {
	metadata := model.Metadata{model.MetadataOrganizationName: "Example Team"}
	profile := model.Profile{Agent: "claude", Label: "unknown", Metadata: metadata}
	account := model.ActiveAccount{Agent: "claude", Label: "unknown", Metadata: metadata}

	if SameLogicalAccount(profile, account) {
		t.Fatal("logins without a readable name matched by organization name alone")
	}
}

func TestSameLogicalAccountSeparatesTokensFromBrowserLogins(t *testing.T) {
	browser := model.Metadata{
		model.MetadataAccountUUID: "account-1", model.MetadataOrganizationUUID: "team-org",
		model.MetadataCredentialKind: CredentialKindOAuth,
	}
	token := func(id string) model.Metadata {
		return model.Metadata{model.MetadataCredentialKind: CredentialKindToken, model.MetadataTokenID: id}
	}
	tests := []struct {
		name    string
		profile model.Profile
		account model.ActiveAccount
		want    bool
	}{
		{
			name:    "browser login and token with the same label",
			profile: model.Profile{Agent: "claude", Label: "same@example.com", Metadata: browser},
			account: model.ActiveAccount{Agent: "claude", Label: "same@example.com", Metadata: token("token-1")},
			want:    false,
		},
		{
			name:    "token and browser login with the same label",
			profile: model.Profile{Agent: "claude", Label: "work", Metadata: token("token-1")},
			account: model.ActiveAccount{Agent: "claude", Label: "work", Metadata: model.Metadata{model.MetadataCredentialKind: CredentialKindOAuth}},
			want:    false,
		},
		{
			name:    "same token under another label",
			profile: model.Profile{Agent: "claude", Label: "work", Metadata: token("token-1")},
			account: model.ActiveAccount{Agent: "claude", Label: "stale@example.com", Metadata: token("token-1")},
			want:    true,
		},
		{
			name:    "another token under the same label",
			profile: model.Profile{Agent: "claude", Label: "work", Metadata: token("token-1")},
			account: model.ActiveAccount{Agent: "claude", Label: "work", Metadata: token("token-2")},
			want:    false,
		},
		{
			name:    "tokens without an id",
			profile: model.Profile{Agent: "claude", Label: "work", Metadata: token("")},
			account: model.ActiveAccount{Agent: "claude", Label: "work", Metadata: token("")},
			want:    false,
		},
		{
			name:    "kind unknown on one side",
			profile: model.Profile{Agent: "claude", Label: "work", Metadata: model.Metadata{}},
			account: model.ActiveAccount{Agent: "claude", Label: "work", Metadata: token("token-1")},
			want:    true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := SameLogicalAccount(test.profile, test.account); got != test.want {
				t.Fatalf("SameLogicalAccount() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestClaudeCredentialKind(t *testing.T) {
	tests := map[string]string{
		`{"claudeAiOauth":{"accessToken":"access","refreshToken":"refresh","scopes":["user:inference","user:profile"]}}`: CredentialKindOAuth,
		`{"claudeAiOauth":{"accessToken":"access","refreshToken":"refresh"}}`:                                            CredentialKindOAuth,
		`{"claudeAiOauth":{"accessToken":"sk-ant-oat01-test","refreshToken":null,"scopes":["user:inference"]}}`:          CredentialKindToken,
		`{"claudeAiOauth":{"accessToken":"sk-ant-oat01-test","scopes":["user:inference"]},"mcpOAuth":{"s":{"a":"b"}}}`:   CredentialKindToken,
		`{"claudeAiOauth":{"accessToken":"access","scopes":["user:inference","user:profile"]}}`:                          "",
		`{"claudeAiOauth":{"accessToken":"access"}}`:                                                                     "",
		`{"claudeAiOauth":{"refreshToken":"refresh"}}`:                                                                   "",
		`{"accessToken":"flat"}`: "",
		`not json`:               "",
		``:                       "",
	}
	for credentials, want := range tests {
		if got := ClaudeCredentialKind(credentials); got != want {
			t.Fatalf("ClaudeCredentialKind(%q) = %q, want %q", credentials, got, want)
		}
	}
}

func TestClaudeTokenIDFollowsTheTokenAlone(t *testing.T) {
	token := `{"claudeAiOauth":{"accessToken":"sk-ant-oat01-test","refreshToken":null,"scopes":["user:inference"],"expiresAt":1}}`
	sameTokenLater := `{"claudeAiOauth":{"accessToken":"sk-ant-oat01-test","scopes":["user:inference"],"expiresAt":2},"mcpOAuth":{"s":{"a":"b"}}}`
	otherToken := `{"claudeAiOauth":{"accessToken":"sk-ant-oat01-other","refreshToken":null,"scopes":["user:inference"]}}`
	browser := `{"claudeAiOauth":{"accessToken":"sk-ant-oat01-test","refreshToken":"refresh"}}`

	id := ClaudeTokenID(token)
	if id == "" || strings.Contains(id, "test") || id != ClaudeTokenID(sameTokenLater) || id == ClaudeTokenID(otherToken) || ClaudeTokenID(browser) != "" {
		t.Fatalf("ids: token=%q later=%q other=%q browser=%q", id, ClaudeTokenID(sameTokenLater), ClaudeTokenID(otherToken), ClaudeTokenID(browser))
	}
}

func TestSameClaudeUserIgnoresOrganization(t *testing.T) {
	account := model.ActiveAccount{Agent: "claude", Metadata: model.Metadata{
		model.MetadataAccountUUID: "account-1", model.MetadataOrganizationUUID: "team-org",
	}}
	otherOrganization := model.Profile{Agent: "claude", Metadata: model.Metadata{
		model.MetadataAccountUUID: "Account-1", model.MetadataOrganizationUUID: "enterprise-org",
	}}
	otherUser := model.Profile{Agent: "claude", Metadata: model.Metadata{
		model.MetadataAccountUUID: "account-2", model.MetadataOrganizationUUID: "team-org",
	}}
	noIdentity := model.Profile{Agent: "claude", Metadata: model.Metadata{}}

	if !SameClaudeUser(otherOrganization, account) || SameClaudeUser(otherUser, account) || SameClaudeUser(noIdentity, account) {
		t.Fatal("user identity was not compared by account alone")
	}
}
