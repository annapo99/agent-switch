package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"

	"github.com/annapo99/agent-switch/internal/model"
)

func LabelKey(agent, label string) model.MetadataKey {
	return model.MetadataKey{Agent: agent, Label: normalized(label)}
}

func OrganizationKeys(agent, label string, metadata model.Metadata) []model.MetadataKey {
	base := LabelKey(agent, label)
	keys := []model.MetadataKey{}
	if name := normalized(metadata.String(model.MetadataOrganizationName)); name != "" {
		key := base
		key.Organization = "name:" + name
		keys = append(keys, key)
	}
	if uuid := normalized(metadata.String(model.MetadataOrganizationUUID)); uuid != "" {
		key := base
		key.Organization = "uuid:" + uuid
		keys = append(keys, key)
	}
	if id := normalized(metadata.String(model.MetadataCodexAccountID)); id != "" {
		key := base
		key.Organization = "codex:" + id
		keys = append(keys, key)
	}
	return keys
}

func StrongestKey(agent, label string, metadata model.Metadata) model.MetadataKey {
	keys := OrganizationKeys(agent, label, metadata)
	if len(keys) == 0 {
		return LabelKey(agent, label)
	}
	return keys[len(keys)-1]
}

func HasStableClaudeIdentity(agent string, metadata model.Metadata) bool {
	return agent == "claude" &&
		normalized(metadata.String(model.MetadataAccountUUID)) != "" &&
		normalized(metadata.String(model.MetadataOrganizationUUID)) != ""
}

const (
	CredentialKindOAuth = "oauth"
	CredentialKindToken = "token"
)

func ClaudeCredentialKind(credentials string) string {
	oauth := claudeOAuth(credentials)
	switch {
	case oauth == nil || oauth.AccessToken == "":
		return ""
	case oauth.RefreshToken != "":
		return CredentialKindOAuth
	case len(oauth.Scopes) > 0 && !slices.Contains(oauth.Scopes, "user:profile"):
		return CredentialKindToken
	}
	return ""
}

func ClaudeTokenID(credentials string) string {
	if ClaudeCredentialKind(credentials) != CredentialKindToken {
		return ""
	}
	return ClaudeAccessTokenID(claudeOAuth(credentials).AccessToken)
}

func ClaudeAccessTokenID(accessToken string) string {
	sum := sha256.Sum256([]byte("claude-token:" + accessToken))
	return hex.EncodeToString(sum[:8])
}

type claudeOAuthCredentials struct {
	AccessToken  string   `json:"accessToken"`
	RefreshToken string   `json:"refreshToken"`
	Scopes       []string `json:"scopes"`
}

func claudeOAuth(credentials string) *claudeOAuthCredentials {
	var data struct {
		OAuth *claudeOAuthCredentials `json:"claudeAiOauth"`
	}
	if json.Unmarshal([]byte(credentials), &data) != nil {
		return nil
	}
	return data.OAuth
}

func SameClaudeUser(profile model.Profile, account model.ActiveAccount) bool {
	profileAccount := normalized(profile.Metadata.String(model.MetadataAccountUUID))
	return profile.Agent == "claude" && account.Agent == "claude" &&
		profileAccount != "" && profileAccount == normalized(account.Metadata.String(model.MetadataAccountUUID))
}

func SameLogicalAccount(profile model.Profile, account model.ActiveAccount) bool {
	if profile.Agent != account.Agent {
		return false
	}
	if profile.Agent != "claude" {
		profileID := normalized(profile.Metadata.String(model.MetadataCodexAccountID))
		accountID := normalized(account.Metadata.String(model.MetadataCodexAccountID))
		if profileID != "" && accountID != "" && profileID != accountID {
			return false
		}
		return SameLabel(profile.Label, account.Label)
	}
	profileKind := profile.Metadata.String(model.MetadataCredentialKind)
	accountKind := account.Metadata.String(model.MetadataCredentialKind)
	if profileKind != "" && accountKind != "" && profileKind != accountKind {
		return false
	}
	if profileKind == CredentialKindToken && accountKind == CredentialKindToken {
		tokenID := profile.Metadata.String(model.MetadataTokenID)
		return tokenID != "" && tokenID == account.Metadata.String(model.MetadataTokenID)
	}

	profileAccount := normalized(profile.Metadata.String(model.MetadataAccountUUID))
	accountAccount := normalized(account.Metadata.String(model.MetadataAccountUUID))
	profileOrg := normalized(profile.Metadata.String(model.MetadataOrganizationUUID))
	accountOrg := normalized(account.Metadata.String(model.MetadataOrganizationUUID))
	if profileAccount != "" && accountAccount != "" && profileOrg != "" && accountOrg != "" {
		return profileAccount == accountAccount && profileOrg == accountOrg
	}

	profileName := normalized(profile.Metadata.String(model.MetadataOrganizationName))
	accountName := normalized(account.Metadata.String(model.MetadataOrganizationName))
	if profileName != "" && accountName != "" {
		return SameLabel(profile.Label, account.Label) && profileName == accountName
	}
	if profileOrg != "" || accountOrg != "" || profileName != "" || accountName != "" {
		return false
	}
	return SameLabel(profile.Label, account.Label)
}

func SameLabel(left, right string) bool {
	label := normalized(left)
	return label != "" && label != "unknown" && label == normalized(right)
}

func normalized(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
