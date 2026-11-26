package identity

import (
	"encoding/base64"
	"testing"

	"github.com/annapo99/agent-switch/internal/model"
)

func TestCodexAccountID(t *testing.T) {
	claims := base64.RawURLEncoding.EncodeToString([]byte(`{"email":"a@example.com","https://api.openai.com/auth":{"chatgpt_account_id":"from-id-token"}}`))
	tests := map[string]string{
		`{"tokens":{"account_id":"acct-1","id_token":"x.` + claims + `.y"}}`: "acct-1",
		`{"tokens":{"id_token":"x.` + claims + `.y"}}`:                       "from-id-token",
		`{"tokens":{"access_token":"t"}}`:                                    "",
		`not json`:                                                           "",
	}
	for auth, want := range tests {
		if got := CodexAccountID([]byte(auth)); got != want {
			t.Fatalf("CodexAccountID(%s) = %q, want %q", auth, got, want)
		}
	}
}

func TestSameLogicalAccountKeepsCodexWorkspacesOfOneEmailApart(t *testing.T) {
	profile := model.Profile{Agent: "codex", Label: "a@example.com", Metadata: model.Metadata{model.MetadataCodexAccountID: "personal"}}
	work := model.ActiveAccount{Agent: "codex", Label: "a@example.com", Metadata: model.Metadata{model.MetadataCodexAccountID: "business"}}
	same := model.ActiveAccount{Agent: "codex", Label: "A@example.com", Metadata: model.Metadata{model.MetadataCodexAccountID: "personal"}}
	legacy := model.ActiveAccount{Agent: "codex", Label: "a@example.com", Metadata: model.Metadata{}}

	if SameLogicalAccount(profile, work) || !SameLogicalAccount(profile, same) || !SameLogicalAccount(profile, legacy) {
		t.Fatal("codex workspaces were not told apart by account id")
	}
}
