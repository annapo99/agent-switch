package provider

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/annapo99/agent-switch/internal/model"
)

const ClaudeKeychainService = "Claude Code-credentials"

var emailRE = regexp.MustCompile(`[^@\s]+@[^@\s]+\.[^@\s]+`)

type Provider interface {
	Agent() string
	DisplayName() string
	Detect(home string) (model.ActiveAccount, bool)
	SaveSnapshot(home string, account model.ActiveAccount, profileDir string) error
	ApplySnapshot(home string, profileDir string) error
	MatchesProfile(home string, profile model.Profile) bool
}

type KeychainItem struct {
	Account string `json:"account"`
	Secret  string `json:"secret"`
}

type KeychainBackend interface {
	IsAvailable(home string) bool
	ReadItem(service string) (*KeychainItem, error)
	WriteItem(service, account, secret string) error
}

type SecurityKeychainBackend struct{}

func (SecurityKeychainBackend) IsAvailable(home string) bool {
	current, err := os.UserHomeDir()
	return runtime.GOOS == "darwin" && err == nil && filepath.Clean(home) == filepath.Clean(current)
}

func (SecurityKeychainBackend) ReadItem(service string) (*KeychainItem, error) {
	secretCmd := exec.Command("security", "find-generic-password", "-s", service, "-w")
	secret, err := secretCmd.Output()
	if err != nil {
		return nil, err
	}
	account := service
	meta, err := exec.Command("security", "find-generic-password", "-s", service).CombinedOutput()
	if err == nil {
		re := regexp.MustCompile(`"acct"<blob>="([^"]*)"`)
		if match := re.FindStringSubmatch(string(meta)); len(match) == 2 && match[1] != "" {
			account = match[1]
		}
	}
	return &KeychainItem{Account: account, Secret: strings.TrimRight(string(secret), "\n")}, nil
}

func (SecurityKeychainBackend) WriteItem(service, account, secret string) error {
	cmd := exec.Command("security", "add-generic-password", "-U", "-s", service, "-a", account, "-w", secret)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if len(output) > 0 {
			return fmt.Errorf("%s", strings.TrimSpace(string(output)))
		}
		return err
	}
	return nil
}

type FileAuthProvider struct {
	agent       string
	displayName string
	authPaths   []string
}

func NewFileAuthProvider(agent, displayName string, authPaths []string) *FileAuthProvider {
	return &FileAuthProvider{agent: agent, displayName: displayName, authPaths: authPaths}
}

func (p *FileAuthProvider) Agent() string { return p.agent }
func (p *FileAuthProvider) DisplayName() string {
	return p.displayName
}

func (p *FileAuthProvider) Detect(home string) (model.ActiveAccount, bool) {
	var existing []string
	for _, rel := range p.authPaths {
		if fileExists(filepath.Join(home, filepath.FromSlash(rel))) {
			existing = append(existing, rel)
		}
	}
	if len(existing) == 0 {
		return model.ActiveAccount{}, false
	}
	return model.ActiveAccount{
		Agent:       p.agent,
		DisplayName: p.displayName,
		Label:       p.extractLabel(home, existing),
		Fingerprint: p.fingerprint(home, existing),
		Source:      "~/" + existing[0],
		AuthFiles:   append([]string{}, existing...),
		Metadata:    model.Metadata{},
	}, true
}

func (p *FileAuthProvider) SaveSnapshot(home string, account model.ActiveAccount, profileDir string) error {
	for _, rel := range account.AuthFiles {
		src := filepath.Join(home, filepath.FromSlash(rel))
		dst := filepath.Join(profileDir, "files", filepath.FromSlash(rel))
		if err := copyFile(src, dst); err != nil {
			return err
		}
	}
	return nil
}

func (p *FileAuthProvider) ApplySnapshot(home string, profileDir string) error {
	filesDir := filepath.Join(profileDir, "files")
	return filepath.WalkDir(filesDir, func(path string, entry os.DirEntry, err error) error {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil || entry == nil || entry.IsDir() {
			return err
		}
		rel, err := filepath.Rel(filesDir, path)
		if err != nil {
			return err
		}
		return copyFile(path, filepath.Join(home, rel))
	})
}

func (p *FileAuthProvider) MatchesProfile(home string, profile model.Profile) bool {
	active, ok := p.Detect(home)
	return ok && active.Fingerprint == profile.Fingerprint
}

func (p *FileAuthProvider) extractLabel(home string, rels []string) string {
	for _, rel := range rels {
		data, ok := readJSON(filepath.Join(home, filepath.FromSlash(rel)))
		if !ok {
			continue
		}
		if email := findEmail(data); email != "" {
			return email
		}
		if label := findJWTLabel(data); label != "" {
			return label
		}
		if named := findNamedString(data, map[string]bool{
			"account_id": true,
			"login":      true,
			"username":   true,
			"account":    true,
			"id":         true,
			"sub":        true,
		}); named != "" {
			return named
		}
	}
	return "unknown"
}

func (p *FileAuthProvider) fingerprint(home string, rels []string) string {
	hash := sha256.New()
	hash.Write([]byte(p.agent))
	sort.Strings(rels)
	for _, rel := range rels {
		hash.Write([]byte(rel))
		data, _ := os.ReadFile(filepath.Join(home, filepath.FromSlash(rel)))
		hash.Write(data)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

type ClaudeProvider struct {
	file     *FileAuthProvider
	keychain KeychainBackend
}

func NewClaudeProvider(keychain KeychainBackend) *ClaudeProvider {
	return &ClaudeProvider{
		file:     NewFileAuthProvider("claude", "Claude", []string{".claude/.credentials.json"}),
		keychain: keychain,
	}
}

func (p *ClaudeProvider) Agent() string       { return "claude" }
func (p *ClaudeProvider) DisplayName() string { return "Claude" }

func (p *ClaudeProvider) Detect(home string) (model.ActiveAccount, bool) {
	if item := p.readKeychainItem(home); item != nil {
		metadata := claudeJSONMetadata(home)
		secret := item.Secret
		sum := sha256.Sum256([]byte("claude:" + ClaudeKeychainService + ":" + secret))
		return model.ActiveAccount{
			Agent:       "claude",
			DisplayName: "Claude",
			Label:       p.keychainLabel(home, item),
			Fingerprint: hex.EncodeToString(sum[:]),
			Source:      "Keychain: " + ClaudeKeychainService,
			AuthFiles:   []string{"keychain:" + ClaudeKeychainService},
			Metadata:    metadata,
		}, true
	}
	account, ok := p.file.Detect(home)
	if !ok {
		return model.ActiveAccount{}, false
	}
	account.Metadata = claudeJSONMetadata(home)
	return account, true
}

func (p *ClaudeProvider) SaveSnapshot(home string, account model.ActiveAccount, profileDir string) error {
	if isKeychainAccount(account) {
		item := p.readKeychainItem(home)
		if item == nil {
			return nil
		}
		if err := os.MkdirAll(profileDir, 0o755); err != nil {
			return err
		}
		data, err := json.MarshalIndent(map[string]string{
			"service": ClaudeKeychainService,
			"account": item.Account,
			"secret":  item.Secret,
		}, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(profileDir, "keychain.json"), append(data, '\n'), 0o600)
	}
	return p.file.SaveSnapshot(home, account, profileDir)
}

func (p *ClaudeProvider) ApplySnapshot(home string, profileDir string) error {
	data, err := os.ReadFile(filepath.Join(profileDir, "keychain.json"))
	if err == nil && p.keychain != nil {
		var snapshot map[string]string
		if err := json.Unmarshal(data, &snapshot); err != nil {
			return err
		}
		return p.keychain.WriteItem(snapshot["service"], snapshot["account"], snapshot["secret"])
	}
	return p.file.ApplySnapshot(home, profileDir)
}

func (p *ClaudeProvider) MatchesProfile(home string, profile model.Profile) bool {
	active, ok := p.Detect(home)
	return ok && active.Fingerprint == profile.Fingerprint
}

func (p *ClaudeProvider) readKeychainItem(home string) *KeychainItem {
	if p.keychain == nil || !p.keychain.IsAvailable(home) {
		return nil
	}
	item, err := p.keychain.ReadItem(ClaudeKeychainService)
	if err != nil {
		return nil
	}
	return item
}

func (p *ClaudeProvider) keychainLabel(home string, item *KeychainItem) string {
	if label := claudeJSONLabel(home); label != "" {
		return label
	}
	if item.Account != "" {
		return item.Account
	}
	var data any
	if json.Unmarshal([]byte(item.Secret), &data) == nil {
		if email := findEmail(data); email != "" {
			return email
		}
		if named := findNamedString(data, map[string]bool{"account_id": true, "login": true, "username": true, "sub": true}); named != "" {
			return named
		}
	}
	return "Claude Keychain"
}

type CodexProvider struct {
	file *FileAuthProvider
}

func NewCodexProvider() *CodexProvider {
	return &CodexProvider{file: NewFileAuthProvider("codex", "Codex", []string{".codex/auth.json"})}
}

func (p *CodexProvider) Agent() string       { return "codex" }
func (p *CodexProvider) DisplayName() string { return "Codex" }
func (p *CodexProvider) Detect(home string) (model.ActiveAccount, bool) {
	return p.file.Detect(home)
}
func (p *CodexProvider) SaveSnapshot(home string, account model.ActiveAccount, profileDir string) error {
	return p.file.SaveSnapshot(home, account, profileDir)
}
func (p *CodexProvider) ApplySnapshot(home string, profileDir string) error {
	return p.file.ApplySnapshot(home, profileDir)
}
func (p *CodexProvider) MatchesProfile(home string, profile model.Profile) bool {
	return p.file.MatchesProfile(home, profile)
}

func DefaultProviders() []Provider {
	return []Provider{NewClaudeProvider(SecurityKeychainBackend{}), NewCodexProvider()}
}

func ProviderByAgent(agent string) Provider {
	for _, provider := range DefaultProviders() {
		if provider.Agent() == agent {
			return provider
		}
	}
	return nil
}

func isKeychainAccount(account model.ActiveAccount) bool {
	for _, rel := range account.AuthFiles {
		if strings.HasPrefix(rel, "keychain:") {
			return true
		}
	}
	return false
}

func claudeJSONLabel(home string) string {
	oauth := claudeOAuthAccount(home)
	for _, key := range []string{"emailAddress", "email", "userEmail"} {
		if value, ok := oauth[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return findEmail(oauth)
}

func claudeJSONMetadata(home string) model.Metadata {
	oauth := claudeOAuthAccount(home)
	metadata := model.Metadata{}
	mapping := map[string]string{
		"organizationName":          "organization_name",
		"organizationRateLimitTier": "organization_rate_limit_tier",
		"userRateLimitTier":         "user_rate_limit_tier",
		"seatTier":                  "seat_tier",
	}
	for source, target := range mapping {
		if value, ok := oauth[source].(string); ok && strings.TrimSpace(value) != "" {
			metadata[target] = strings.TrimSpace(value)
		}
	}
	return metadata
}

func claudeOAuthAccount(home string) map[string]any {
	data, ok := readJSON(filepath.Join(home, ".claude.json"))
	if !ok {
		return nil
	}
	root, ok := data.(map[string]any)
	if !ok {
		return nil
	}
	oauth, ok := root["oauthAccount"].(map[string]any)
	if !ok {
		return nil
	}
	return oauth
}

func readJSON(path string) (any, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, false
	}
	return value, true
}

func findEmail(value any) string {
	switch item := value.(type) {
	case string:
		return emailRE.FindString(item)
	case map[string]any:
		for _, key := range []string{"email", "user_email", "userEmail", "emailAddress"} {
			if text, ok := item[key].(string); ok {
				if email := emailRE.FindString(text); email != "" {
					return email
				}
			}
		}
		for _, child := range item {
			if email := findEmail(child); email != "" {
				return email
			}
		}
	case []any:
		for _, child := range item {
			if email := findEmail(child); email != "" {
				return email
			}
		}
	}
	return ""
}

func findJWTLabel(value any) string {
	switch item := value.(type) {
	case string:
		payload := decodeJWT(item)
		if payload == nil {
			return ""
		}
		if email := findEmail(payload); email != "" {
			return email
		}
		return findNamedString(payload, map[string]bool{"account_id": true, "sub": true, "name": true})
	case map[string]any:
		for _, child := range item {
			if found := findJWTLabel(child); found != "" {
				return found
			}
		}
	case []any:
		for _, child := range item {
			if found := findJWTLabel(child); found != "" {
				return found
			}
		}
	}
	return ""
}

func decodeJWT(token string) map[string]any {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		payload, err = base64.URLEncoding.DecodeString(parts[1] + strings.Repeat("=", (4-len(parts[1])%4)%4))
		if err != nil {
			return nil
		}
	}
	var out map[string]any
	if err := json.Unmarshal(payload, &out); err != nil {
		return nil
	}
	return out
}

func findNamedString(value any, keys map[string]bool) string {
	switch item := value.(type) {
	case map[string]any:
		for key := range keys {
			if text, ok := item[key].(string); ok && strings.TrimSpace(text) != "" {
				return strings.TrimSpace(text)
			}
		}
		for _, child := range item {
			if found := findNamedString(child, keys); found != "" {
				return found
			}
		}
	case []any:
		for _, child := range item {
			if found := findNamedString(child, keys); found != "" {
				return found
			}
		}
	}
	return ""
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o600)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
