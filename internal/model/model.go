package model

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
	Agent       string   `json:"agent"`
	DisplayName string   `json:"display_name"`
	Number      int      `json:"number"`
	Label       string   `json:"label"`
	Fingerprint string   `json:"fingerprint"`
	Source      string   `json:"source"`
	AuthFiles   []string `json:"auth_files"`
	CreatedAt   string   `json:"created_at"`
	Metadata    Metadata `json:"metadata"`
}

type MetadataKey struct {
	Agent string
	Label string
}
