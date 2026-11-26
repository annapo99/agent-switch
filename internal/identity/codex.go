package identity

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

func CodexAccountID(auth []byte) string {
	var data struct {
		Tokens struct {
			AccountID string `json:"account_id"`
			IDToken   string `json:"id_token"`
		} `json:"tokens"`
	}
	if json.Unmarshal(auth, &data) != nil {
		return ""
	}
	if data.Tokens.AccountID != "" {
		return data.Tokens.AccountID
	}
	parts := strings.Split(data.Tokens.IDToken, ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return ""
	}
	var claims struct {
		Auth struct {
			AccountID string `json:"chatgpt_account_id"`
		} `json:"https://api.openai.com/auth"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return ""
	}
	return claims.Auth.AccountID
}
