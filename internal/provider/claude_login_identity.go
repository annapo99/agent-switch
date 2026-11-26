package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/annapo99/agent-switch/internal/identity"
)

var ClaudeProfileURL = "https://api.anthropic.com/api/oauth/profile"

const claudeExpiryMarginMillis = 60 * 1000

var ErrClaudeLoginUnverified = errors.New("could not confirm which Claude account the Keychain login belongs to")

type ClaudeLoginIdentity struct {
	AccountUUID      string
	OrganizationUUID string
}

func FetchClaudeLoginIdentity(accessToken string) (ClaudeLoginIdentity, error) {
	request, err := http.NewRequest(http.MethodGet, ClaudeProfileURL, nil)
	if err != nil {
		return ClaudeLoginIdentity{}, err
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "agent-switch")
	client := http.Client{
		Timeout:       8 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	response, err := client.Do(request)
	if err != nil {
		return ClaudeLoginIdentity{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ClaudeLoginIdentity{}, fmt.Errorf("profile request returned HTTP %d", response.StatusCode)
	}
	var profile struct {
		Account struct {
			UUID string `json:"uuid"`
		} `json:"account"`
		Organization struct {
			UUID string `json:"uuid"`
		} `json:"organization"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&profile); err != nil {
		return ClaudeLoginIdentity{}, err
	}
	if profile.Account.UUID == "" || profile.Organization.UUID == "" {
		return ClaudeLoginIdentity{}, errors.New("profile response has no account or organization")
	}
	return ClaudeLoginIdentity{AccountUUID: profile.Account.UUID, OrganizationUUID: profile.Organization.UUID}, nil
}

func (p *ClaudeProvider) VerifyLogin(home string, account ActiveAccountIdentity, fetch func(accessToken string) (ClaudeLoginIdentity, error), nowMillis int64) error {
	credentials, ok := p.loginWithFingerprint(home, account.Fingerprint)
	if !ok {
		return ErrClaudeLoginUnverified
	}
	var data struct {
		OAuth struct {
			AccessToken string   `json:"accessToken"`
			ExpiresAt   *float64 `json:"expiresAt"`
		} `json:"claudeAiOauth"`
	}
	if json.Unmarshal([]byte(credentials), &data) != nil || data.OAuth.AccessToken == "" {
		return ErrClaudeLoginUnverified
	}
	if data.OAuth.ExpiresAt != nil && int64(*data.OAuth.ExpiresAt)-claudeExpiryMarginMillis < nowMillis {
		return ErrClaudeLoginUnverified
	}
	got, err := fetch(data.OAuth.AccessToken)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrClaudeLoginUnverified, err)
	}
	if !strings.EqualFold(got.AccountUUID, account.AccountUUID) || !strings.EqualFold(got.OrganizationUUID, account.OrganizationUUID) {
		return fmt.Errorf("%w: the Keychain login belongs to another account or organization than ~/.claude.json", ErrClaudeLoginUnverified)
	}
	return nil
}

func (p *ClaudeProvider) loginWithFingerprint(home, fingerprint string) (string, bool) {
	if item := p.readKeychainItem(home); item != nil {
		return item.Secret, identity.ClaudeKeychainFingerprint(item.Secret) == fingerprint
	}
	data, err := os.ReadFile(filepath.Join(home, ".claude", ".credentials.json"))
	if err != nil {
		return "", false
	}
	return string(data), p.file.fingerprintOf(map[string][]byte{".claude/.credentials.json": data}) == fingerprint
}

type ActiveAccountIdentity struct {
	Fingerprint      string
	AccountUUID      string
	OrganizationUUID string
}
