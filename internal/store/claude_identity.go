package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/annapo99/agent-switch/internal/identity"
	"github.com/annapo99/agent-switch/internal/model"
)

func withClaudeCredentialKind(profile model.Profile, profileDir string) model.Profile {
	if profile.Agent != "claude" {
		return profile
	}
	credentials := claudeCredentials(profileDir)
	delete(profile.Metadata, model.MetadataCredentialKind)
	delete(profile.Metadata, model.MetadataTokenID)
	if kind := identity.ClaudeCredentialKind(credentials); kind != "" {
		profile.Metadata[model.MetadataCredentialKind] = kind
	}
	if tokenID := identity.ClaudeTokenID(credentials); tokenID != "" {
		profile.Metadata[model.MetadataTokenID] = tokenID
	}
	return profile
}

func claudeCredentials(profileDir string) string {
	if data, err := os.ReadFile(filepath.Join(profileDir, "keychain.json")); err == nil {
		var snapshot struct {
			Secret string `json:"secret"`
		}
		if json.Unmarshal(data, &snapshot) == nil && snapshot.Secret != "" {
			return snapshot.Secret
		}
	}
	data, err := os.ReadFile(filepath.Join(profileDir, "files", ".claude", ".credentials.json"))
	if err != nil {
		return ""
	}
	return string(data)
}

func hydrateClaudeIdentity(profile model.Profile, profileDir string) (model.Profile, error) {
	if profile.Agent != "claude" ||
		(profile.Metadata.String(model.MetadataAccountUUID) != "" &&
			profile.Metadata.String(model.MetadataOrganizationUUID) != "") {
		return profile, nil
	}
	data, err := os.ReadFile(filepath.Join(profileDir, "config.json"))
	if err != nil {
		return profile, nil
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return profile, nil
	}
	oauth, _ := root["oauthAccount"].(map[string]any)
	next := profile
	next.Metadata = cloneMetadata(profile.Metadata)
	changed := copyStringMetadata(next.Metadata, oauth, "accountUuid", model.MetadataAccountUUID)
	changed = copyStringMetadata(next.Metadata, oauth, "organizationUuid", model.MetadataOrganizationUUID) || changed
	if !changed {
		return profile, nil
	}
	if err := writeProfileAtomic(filepath.Join(profileDir, "manifest.json"), next); err != nil {
		return profile, err
	}
	return next, nil
}

func copyStringMetadata(metadata model.Metadata, source map[string]any, sourceKey, targetKey string) bool {
	if metadata.String(targetKey) != "" {
		return false
	}
	value, _ := source[sourceKey].(string)
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	metadata[targetKey] = value
	return true
}

func writeProfileAtomic(path string, profile model.Profile) error {
	data, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".manifest-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
