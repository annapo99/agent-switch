package identity

import (
	"strings"
	"testing"
)

func TestClaudeKeychainFingerprintIgnoresMCPLogins(t *testing.T) {
	login := `{"claudeAiOauth":{"accessToken":"access","refreshToken":"refresh","expiresAt":1795667708000}}`
	withMCP := `{"mcpOAuth":{"notion":{"refreshToken":"mcp-1"}}, "claudeAiOauth":{"expiresAt":1795667708000,"refreshToken":"refresh","accessToken":"access"}}`
	refreshedMCP := `{"claudeAiOauth":{"accessToken":"access","refreshToken":"refresh","expiresAt":1795667708000},"mcpOAuth":{"notion":{"refreshToken":"mcp-2"}}}`
	otherLogin := `{"claudeAiOauth":{"accessToken":"access","refreshToken":"refresh","expiresAt":1795667708001},"mcpOAuth":{"notion":{"refreshToken":"mcp-1"}}}`

	fingerprint := ClaudeKeychainFingerprint(login)
	if fingerprint != ClaudeKeychainFingerprint(withMCP) || fingerprint != ClaudeKeychainFingerprint(refreshedMCP) {
		t.Fatal("an MCP server login changed the account fingerprint")
	}
	if fingerprint == ClaudeKeychainFingerprint(otherLogin) {
		t.Fatal("a different account login kept the same fingerprint")
	}
	if ClaudeKeychainFingerprint("not json") == ClaudeKeychainFingerprint("other") {
		t.Fatal("unparsed credentials collapsed into one fingerprint")
	}
}

func TestClaudeLoginCredentialsDropsMCPLogins(t *testing.T) {
	login := `{"claudeAiOauth":{"accessToken":"access"}}`
	if got := ClaudeLoginCredentials(login); got != login {
		t.Fatalf("credentials without MCP logins changed: %s", got)
	}
	got := ClaudeLoginCredentials(`{"claudeAiOauth":{"accessToken":"access"},"mcpOAuth":{"notion":{"serverUrl":"https://mcp.example.com/a?b=1&c=2"}}}`)
	if strings.Contains(got, "mcpOAuth") || !strings.Contains(got, `"accessToken":"access"`) {
		t.Fatalf("ClaudeLoginCredentials() = %s", got)
	}
	if got := ClaudeLoginCredentials("not json"); got != "not json" {
		t.Fatalf("unparsed credentials changed: %s", got)
	}
}

func TestWithClaudeMCPLoginsTakesTheCurrentMCPLogins(t *testing.T) {
	saved := `{"claudeAiOauth":{"accessToken":"saved"},"mcpOAuth":{"notion":{"refreshToken":"old"}}}`
	current := `{"claudeAiOauth":{"accessToken":"current"},"mcpOAuth":{"notion":{"refreshToken":"new","serverUrl":"https://mcp.example.com/a?b=1&c=2"}}}`

	got := WithClaudeMCPLogins(saved, current)
	if !strings.Contains(got, `"accessToken":"saved"`) || strings.Contains(got, `"accessToken":"current"`) {
		t.Fatalf("account login did not come from the saved credentials: %s", got)
	}
	if !strings.Contains(got, `"refreshToken":"new"`) || strings.Contains(got, `"refreshToken":"old"`) || !strings.Contains(got, "a?b=1&c=2") {
		t.Fatalf("MCP logins did not come from the current credentials: %s", got)
	}
	if got := WithClaudeMCPLogins(saved, `{"claudeAiOauth":{"accessToken":"current"}}`); got != saved {
		t.Fatalf("saved MCP logins were dropped while nothing replaced them: %s", got)
	}
	if got := WithClaudeMCPLogins(saved, ""); got != saved {
		t.Fatalf("saved credentials changed without current credentials: %s", got)
	}
}
