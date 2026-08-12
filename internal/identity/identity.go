package identity

import (
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

func SameLogicalAccount(profile model.Profile, account model.ActiveAccount) bool {
	if profile.Agent != account.Agent {
		return false
	}
	if profile.Agent != "claude" {
		return normalized(profile.Label) != "" && normalized(profile.Label) == normalized(account.Label)
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
		return normalized(profile.Label) == normalized(account.Label) && profileName == accountName
	}
	if profileOrg != "" || accountOrg != "" || profileName != "" || accountName != "" {
		return false
	}
	return normalized(profile.Label) != "" && normalized(profile.Label) == normalized(account.Label)
}

func normalized(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
