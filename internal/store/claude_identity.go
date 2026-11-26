package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/annapo99/agent-switch/internal/fsutil"
	"github.com/annapo99/agent-switch/internal/identity"
	"github.com/annapo99/agent-switch/internal/model"
)

func withClaudeCredentialKind(profile model.Profile, profileDir string) model.Profile {
	if profile.Agent != "claude" {
		return profile
	}
	credentials := claudeCredentials(profileDir)
	if secret := claudeKeychainSecret(profileDir); secret != "" {
		profile.Fingerprint = identity.ClaudeKeychainFingerprint(secret)
		if login := identity.ClaudeLoginCredentials(secret); login != secret {
			_ = rewriteClaudeKeychainSecret(profileDir, login)
		}
	}
	delete(profile.Metadata, model.MetadataCredentialKind)
	delete(profile.Metadata, model.MetadataTokenID)
	if kind := identity.ClaudeCredentialKind(credentials); kind != "" {
		profile.Metadata[model.MetadataCredentialKind] = kind
	}
	if tokenID := identity.ClaudeTokenID(credentials); tokenID != "" {
		profile.Metadata[model.MetadataTokenID] = tokenID
		delete(profile.Metadata, model.MetadataAccountUUID)
		delete(profile.Metadata, model.MetadataOrganizationUUID)
		delete(profile.Metadata, model.MetadataOrganizationName)
	}
	return profile
}

func claudeKeychainSecret(profileDir string) string {
	data, err := os.ReadFile(filepath.Join(profileDir, "keychain.json"))
	if err != nil {
		return ""
	}
	var snapshot struct {
		Secret string `json:"secret"`
	}
	if json.Unmarshal(data, &snapshot) != nil {
		return ""
	}
	return snapshot.Secret
}

func rewriteClaudeKeychainSecret(profileDir, secret string) error {
	path := filepath.Join(profileDir, "keychain.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var snapshot map[string]any
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return err
	}
	snapshot["secret"] = secret
	data, err = json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFile(path, append(data, '\n'))
}

func claudeCredentials(profileDir string) string {
	if secret := claudeKeychainSecret(profileDir); secret != "" {
		return secret
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
	if err := writeProfile(filepath.Join(profileDir, "manifest.json"), next); err != nil {
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
