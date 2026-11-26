package model

const (
	MetadataAccountUUID      = "account_uuid"
	MetadataOrganizationUUID = "organization_uuid"
	MetadataOrganizationName = "organization_name"
	MetadataCredentialKind   = "credential_kind"
	MetadataTokenID          = "token_id"
	MetadataCodexAccountID   = "codex_account_id"
)

type Metadata map[string]any

func (m Metadata) String(key string) string {
	value, ok := m[key]
	if !ok {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return text
}

type ActiveAccount struct {
	Agent       string   `json:"agent"`
	DisplayName string   `json:"display_name"`
	Label       string   `json:"label"`
	Fingerprint string   `json:"fingerprint"`
	Source      string   `json:"source"`
	AuthFiles   []string `json:"auth_files"`
	Metadata    Metadata `json:"metadata"`
}

type Profile struct {
	Agent       string `json:"agent"`
	DisplayName string `json:"display_name"`
	Number      int    `json:"number"`
	Label       string `json:"label"`
	// Name is a display name chosen with `ags rename`; it replaces the label
	// and organization in lists and survives credential updates.
	Name        string   `json:"name,omitempty"`
	Fingerprint string   `json:"fingerprint"`
	Source      string   `json:"source"`
	AuthFiles   []string `json:"auth_files"`
	CreatedAt   string   `json:"created_at"`
	Metadata    Metadata `json:"metadata"`
	Active      bool     `json:"active,omitempty"`
}

type SaveCandidate struct {
	Agent           string   `json:"agent"`
	DisplayName     string   `json:"display_name"`
	SaveNumber      int      `json:"save_number,omitempty"`
	DuplicateNumber int      `json:"duplicate_number,omitempty"`
	Label           string   `json:"label"`
	Fingerprint     string   `json:"fingerprint"`
	Source          string   `json:"source"`
	AuthFiles       []string `json:"auth_files"`
	Metadata        Metadata `json:"metadata"`
}

type MetadataKey struct {
	Agent        string
	Label        string
	Organization string
}
