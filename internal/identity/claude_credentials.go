package identity

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

const ClaudeKeychainService = "Claude Code-credentials"

const claudeMCPLoginsKey = "mcpOAuth"

func ClaudeLoginCredentials(credentials string) string {
	fields, ok := credentialFields(credentials)
	if !ok {
		return credentials
	}
	if _, found := fields[claudeMCPLoginsKey]; !found {
		return credentials
	}
	delete(fields, claudeMCPLoginsKey)
	return marshalJSON(fields, credentials)
}

func WithClaudeMCPLogins(credentials, current string) string {
	currentFields, ok := credentialFields(current)
	if !ok {
		return credentials
	}
	logins, found := currentFields[claudeMCPLoginsKey]
	if !found {
		return credentials
	}
	fields, ok := credentialFields(credentials)
	if !ok {
		return credentials
	}
	fields[claudeMCPLoginsKey] = logins
	return marshalJSON(fields, credentials)
}

func ClaudeKeychainFingerprint(credentials string) string {
	login := credentials
	var fields map[string]any
	decoder := json.NewDecoder(bytes.NewReader([]byte(credentials)))
	decoder.UseNumber()
	if decoder.Decode(&fields) == nil && fields != nil {
		delete(fields, claudeMCPLoginsKey)
		login = marshalJSON(fields, credentials)
	}
	sum := sha256.Sum256([]byte("claude:" + ClaudeKeychainService + ":" + login))
	return hex.EncodeToString(sum[:])
}

func credentialFields(credentials string) (map[string]json.RawMessage, bool) {
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(credentials), &fields) != nil || fields == nil {
		return nil, false
	}
	return fields, true
}

func marshalJSON(value any, fallback string) string {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if encoder.Encode(value) != nil {
		return fallback
	}
	return string(bytes.TrimRight(buffer.Bytes(), "\n"))
}
