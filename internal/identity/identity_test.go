package identity

import (
	"testing"

	"github.com/annapo99/agent-switch/internal/model"
)

func TestSameLogicalAccount(t *testing.T) {
	tests := []struct {
		name    string
		profile model.Profile
		account model.ActiveAccount
		want    bool
	}{
		{
			name: "same email different organization uuid",
			profile: model.Profile{Agent: "claude", Label: "same@example.com", Metadata: model.Metadata{
				model.MetadataAccountUUID: "account-1", model.MetadataOrganizationUUID: "team-org",
			}},
			account: model.ActiveAccount{Agent: "claude", Label: "same@example.com", Metadata: model.Metadata{
				model.MetadataAccountUUID: "account-1", model.MetadataOrganizationUUID: "enterprise-org",
			}},
			want: false,
		},
		{
			name: "same account and organization uuid",
			profile: model.Profile{Agent: "claude", Label: "old@example.com", Metadata: model.Metadata{
				model.MetadataAccountUUID: "account-1", model.MetadataOrganizationUUID: "team-org",
			}},
			account: model.ActiveAccount{Agent: "claude", Label: "new@example.com", Metadata: model.Metadata{
				model.MetadataAccountUUID: "account-1", model.MetadataOrganizationUUID: "team-org",
			}},
			want: true,
		},
		{
			name: "legacy organization names differ",
			profile: model.Profile{Agent: "claude", Label: "same@example.com", Metadata: model.Metadata{
				model.MetadataOrganizationName: "Example Team",
			}},
			account: model.ActiveAccount{Agent: "claude", Label: "same@example.com", Metadata: model.Metadata{
				model.MetadataOrganizationName: "Example Enterprise",
			}},
			want: false,
		},
		{
			name: "legacy organization names match",
			profile: model.Profile{Agent: "claude", Label: "SAME@example.com", Metadata: model.Metadata{
				model.MetadataOrganizationName: "Example Team",
			}},
			account: model.ActiveAccount{Agent: "claude", Label: "same@example.com", Metadata: model.Metadata{
				model.MetadataOrganizationName: "example team",
			}},
			want: true,
		},
		{
			name:    "codex keeps label fallback",
			profile: model.Profile{Agent: "codex", Label: "SAME@example.com"},
			account: model.ActiveAccount{Agent: "codex", Label: "same@example.com"},
			want:    true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := SameLogicalAccount(test.profile, test.account); got != test.want {
				t.Fatalf("SameLogicalAccount() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestOrganizationKeysOrdersNameBeforeUUID(t *testing.T) {
	keys := OrganizationKeys("claude", "Same@Example.com", model.Metadata{
		model.MetadataOrganizationName: "Example Team",
		model.MetadataOrganizationUUID: "TEAM-ORG",
	})

	if len(keys) != 2 ||
		keys[0] != (model.MetadataKey{Agent: "claude", Label: "same@example.com", Organization: "name:example team"}) ||
		keys[1] != (model.MetadataKey{Agent: "claude", Label: "same@example.com", Organization: "uuid:team-org"}) {
		t.Fatalf("keys = %#v", keys)
	}
}

func TestSameLogicalAccountIgnoresUnknownLabels(t *testing.T) {
	for _, agent := range []string{"codex", "claude"} {
		profile := model.Profile{Agent: agent, Label: "unknown"}
		account := model.ActiveAccount{Agent: agent, Label: "Unknown"}
		if SameLogicalAccount(profile, account) {
			t.Fatalf("%s profiles without a readable name matched each other", agent)
		}
	}
}

func TestSameLogicalAccountIgnoresUnknownLabelsWithinAnOrganizationName(t *testing.T) {
	metadata := model.Metadata{model.MetadataOrganizationName: "Example Team"}
	profile := model.Profile{Agent: "claude", Label: "unknown", Metadata: metadata}
	account := model.ActiveAccount{Agent: "claude", Label: "unknown", Metadata: metadata}

	if SameLogicalAccount(profile, account) {
		t.Fatal("logins without a readable name matched by organization name alone")
	}
}
