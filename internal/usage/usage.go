package usage

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/annapo99/agent-switch/internal/model"
)

const CodexUsageURL = "https://chatgpt.com/backend-api/wham/usage"
const ClaudeUsageURL = "https://api.anthropic.com/api/oauth/usage"
const ClaudeOAuthBetaHeader = "oauth-2025-04-20"
const ClaudeOAuthTokenURL = "https://platform.claude.com/v1/oauth/token"
const ClaudeOAuthClientID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
const claudeKeychainService = "Claude Code-credentials"
const oauthExpiryBufferSeconds = 5 * 60

var errInvalidGrant = errors.New("invalid_grant")

type Provider interface {
	Load(home string) map[model.MetadataKey]model.Metadata
}

type CompositeProvider struct {
	Providers []Provider
}

func (p CompositeProvider) Load(home string) map[model.MetadataKey]model.Metadata {
	merged := map[model.MetadataKey]model.Metadata{}
	for _, provider := range p.Providers {
		if provider == nil {
			continue
		}
		for key, metadata := range provider.Load(home) {
			if merged[key] == nil {
				merged[key] = model.Metadata{}
			}
			for itemKey, value := range metadata {
				merged[key][itemKey] = value
			}
		}
	}
	return merged
}

func DefaultProvider() Provider {
	return CompositeProvider{Providers: []Provider{
		CswapProvider{RestrictToCurrentHome: true},
		ClaudeOAuthProvider{RestrictToCurrentHome: true},
		CodexProvider{RestrictToCurrentHome: true},
	}}
}

type CswapProvider struct {
	Runner                func([]string) (string, error)
	RestrictToCurrentHome bool
}

func (p CswapProvider) Load(home string) map[model.MetadataKey]model.Metadata {
	if p.RestrictToCurrentHome && !isCurrentHome(home) {
		return nil
	}
	metadata := map[model.MetadataKey]model.Metadata{}
	if text, err := p.run([]string{"cswap", "list", "--json"}); err == nil && text != "" {
		var payload map[string]any
		if json.Unmarshal([]byte(text), &payload) == nil {
			mergeMetadata(metadata, MetadataByEmailFromCswapPayload(payload))
		}
	}
	if text, err := p.run([]string{"cswap", "list", "--token-status"}); err == nil && text != "" {
		mergeMetadata(metadata, TokenStatusByEmailFromCswapText(text))
	}
	return metadata
}

func (p CswapProvider) run(command []string) (string, error) {
	if p.Runner != nil {
		return p.Runner(command)
	}
	cmd := exec.Command(command[0], command[1:]...)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

func MetadataByEmailFromCswapPayload(payload map[string]any) map[model.MetadataKey]model.Metadata {
	metadata := map[model.MetadataKey]model.Metadata{}
	accounts, ok := payload["accounts"].([]any)
	if !ok {
		return metadata
	}
	for _, item := range accounts {
		account, ok := item.(map[string]any)
		if !ok {
			continue
		}
		email := stringValue(account["email"])
		if email == "" {
			continue
		}
		profileMetadata := model.Metadata{}
		if org := stringValue(account["organizationName"]); org != "" {
			profileMetadata["organization_name"] = org
		}
		if usageMap, ok := account["usage"].(map[string]any); ok {
			if rows := usageLimitsFromCswapUsage(usageMap, account["usageAgeSeconds"]); len(rows) > 0 {
				profileMetadata["usage_limits"] = rows
			}
		}
		if len(profileMetadata) > 0 {
			metadata[model.MetadataKey{Agent: "claude", Label: strings.ToLower(email)}] = profileMetadata
		}
	}
	return metadata
}

func TokenStatusByEmailFromCswapText(text string) map[model.MetadataKey]model.Metadata {
	metadata := map[model.MetadataKey]model.Metadata{}
	accountRE := regexp.MustCompile(`^\s*\d+:\s+(\S+)`)
	oauthRE := regexp.MustCompile(`^\s*(?:[├└]\s*)?•\s+(oauth:\s+.+)$`)
	ansiRE := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	currentEmail := ""
	for _, raw := range strings.Split(text, "\n") {
		line := ansiRE.ReplaceAllString(raw, "")
		if match := accountRE.FindStringSubmatch(line); len(match) == 2 {
			currentEmail = strings.ToLower(match[1])
			continue
		}
		if match := oauthRE.FindStringSubmatch(line); len(match) == 2 && currentEmail != "" {
			key := model.MetadataKey{Agent: "claude", Label: currentEmail}
			metadata[key] = model.Metadata{"oauth_status": match[1]}
		}
	}
	return metadata
}

type ClaudeOAuthProvider struct {
	Fetcher               func(accessToken string) (map[string]any, error)
	TokenRefresher        func(credentials string) (string, error)
	RestrictToCurrentHome bool
	NowEpoch              int64
}

func (p ClaudeOAuthProvider) Load(home string) map[model.MetadataKey]model.Metadata {
	if p.RestrictToCurrentHome && !isCurrentHome(home) {
		return nil
	}
	metadata := map[model.MetadataKey]model.Metadata{}
	for _, profileDir := range claudeProfileDirs(home) {
		manifest := readJSON(filepath.Join(profileDir, "manifest.json"))
		label := strings.ToLower(strings.TrimSpace(stringValue(manifest["label"])))
		if label == "" {
			continue
		}
		credentials := claudeCredentialsFromProfileDir(profileDir)
		if credentials == "" {
			continue
		}
		var invalidRefresh bool
		credentials, invalidRefresh = p.refreshIfExpired(profileDir, credentials)
		profileMetadata := model.Metadata{}
		if status := OAuthStatusFromClaudeCredentials(credentials, p.NowEpoch); status != "" {
			if invalidRefresh {
				status = strings.Replace(status, "refresh token yes", "refresh token invalid", 1)
			}
			profileMetadata["oauth_status"] = status
		}
		if !invalidRefresh {
			oauth := claudeOAuthData(credentials)
			if token := stringValue(oauth["accessToken"]); token != "" {
				payload, err := p.fetch(token)
				if err == nil {
					if rows := UsageRowsFromClaudePayload(payload, p.NowEpoch); len(rows) > 0 {
						profileMetadata["usage_limits"] = rows
					}
				}
			}
		}
		if len(profileMetadata) > 0 {
			metadata[model.MetadataKey{Agent: "claude", Label: label}] = profileMetadata
		}
	}
	return metadata
}

func (p ClaudeOAuthProvider) fetch(accessToken string) (map[string]any, error) {
	if p.Fetcher != nil {
		return p.Fetcher(accessToken)
	}
	request, err := http.NewRequest(http.MethodGet, ClaudeUsageURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("anthropic-beta", ClaudeOAuthBetaHeader)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "agent-switch")
	client := http.Client{Timeout: 8 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, errHTTPStatus(response.StatusCode)
	}
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func (p ClaudeOAuthProvider) refreshIfExpired(profileDir, credentials string) (string, bool) {
	if !claudeOAuthExpired(credentials, p.NowEpoch) || stringValue(claudeOAuthData(credentials)["refreshToken"]) == "" {
		return credentials, false
	}
	refreshed, err := p.refresh(credentials)
	if err != nil || refreshed == "" {
		return credentials, errors.Is(err, errInvalidGrant)
	}
	_ = persistClaudeCredentials(profileDir, credentials, refreshed)
	return refreshed, false
}

func (p ClaudeOAuthProvider) refresh(credentials string) (string, error) {
	if p.TokenRefresher != nil {
		return p.TokenRefresher(credentials)
	}
	return refreshClaudeOAuthCredentials(credentials)
}

func UsageRowsFromClaudePayload(payload map[string]any, nowEpoch int64) []any {
	rows := []any{}
	for _, pair := range []struct {
		key   string
		label string
	}{{"five_hour", "5h"}, {"seven_day", "7d"}} {
		window, ok := payload[pair.key].(map[string]any)
		if !ok {
			continue
		}
		if row := rowFromClaudeWindow(pair.label, window["utilization"], window["resets_at"], nowEpoch); row != nil {
			rows = append(rows, row)
		}
	}
	limits, ok := payload["limits"].([]any)
	if !ok {
		return rows
	}
	for _, item := range limits {
		limit, ok := item.(map[string]any)
		if !ok {
			continue
		}
		scope, _ := limit["scope"].(map[string]any)
		modelScope, _ := scope["model"].(map[string]any)
		label := strings.TrimSpace(stringValue(modelScope["display_name"]))
		if label == "" {
			continue
		}
		if row := rowFromClaudeWindow(label, limit["percent"], limit["resets_at"], nowEpoch); row != nil {
			rows = append(rows, row)
		}
	}
	return rows
}

func refreshClaudeOAuthCredentials(credentials string) (string, error) {
	var data map[string]any
	if err := json.Unmarshal([]byte(credentials), &data); err != nil {
		return "", err
	}
	oauth := claudeOAuthData(credentials)
	refreshToken := stringValue(oauth["refreshToken"])
	if refreshToken == "" {
		return "", httpStatusError(0)
	}
	body, err := json.Marshal(map[string]any{
		"grant_type":    "refresh_token",
		"refresh_token": refreshToken,
		"client_id":     ClaudeOAuthClientID,
	})
	if err != nil {
		return "", err
	}
	request, err := http.NewRequest(http.MethodPost, ClaudeOAuthTokenURL, strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "agent-switch")
	client := http.Client{Timeout: 10 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		responseData, _ := io.ReadAll(response.Body)
		if response.StatusCode >= 400 && response.StatusCode < 500 && strings.Contains(string(responseData), "invalid_grant") {
			return "", errInvalidGrant
		}
		return "", httpStatusError(response.StatusCode)
	}
	responseData, err := io.ReadAll(response.Body)
	if err != nil {
		return "", err
	}
	var payload map[string]any
	if err := json.Unmarshal(responseData, &payload); err != nil {
		return "", err
	}
	accessToken := stringValue(payload["access_token"])
	if accessToken == "" {
		return "", httpStatusError(0)
	}
	oauth["accessToken"] = accessToken
	if expiresIn, ok := numberValue(payload["expires_in"]); ok {
		oauth["expiresAt"] = float64(time.Now().UnixMilli()) + expiresIn*1000
	}
	if refreshToken := stringValue(payload["refresh_token"]); refreshToken != "" {
		oauth["refreshToken"] = refreshToken
	}
	if scope := stringValue(payload["scope"]); scope != "" {
		oauth["scopes"] = strings.Fields(scope)
	}
	if _, nested := data["claudeAiOauth"].(map[string]any); nested {
		data["claudeAiOauth"] = oauth
	}
	updated, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	return string(updated), nil
}

func OAuthStatusFromClaudeCredentials(credentials string, nowEpoch int64) string {
	oauth := claudeOAuthData(credentials)
	if len(oauth) == 0 {
		return ""
	}
	refresh := "no"
	if stringValue(oauth["refreshToken"]) != "" {
		refresh = "yes"
	}
	expiresAt, ok := numberValue(oauth["expiresAt"])
	if !ok {
		return "oauth: unknown expiry, refresh token " + refresh
	}
	expiresSeconds := expiresAt
	if expiresSeconds > 1_000_000_000_000 {
		expiresSeconds = expiresAt / 1000
	}
	now := nowEpoch
	if now == 0 {
		now = time.Now().Unix()
	}
	state := "expired"
	if int64(expiresSeconds) > now+oauthExpiryBufferSeconds {
		state = "fresh"
	}
	remaining := remainingFromSeconds(expiresSeconds - float64(now))
	if remaining != "" {
		return "oauth: " + state + ", refresh token " + refresh + ", expires " + clockFromEpoch(expiresSeconds, now) + " " + remaining
	}
	return "oauth: " + state + ", refresh token " + refresh + ", expires " + clockFromEpoch(expiresSeconds, now)
}

func claudeOAuthExpired(credentials string, nowEpoch int64) bool {
	oauth := claudeOAuthData(credentials)
	expiresAt, ok := numberValue(oauth["expiresAt"])
	if !ok {
		return false
	}
	expiresSeconds := expiresAt
	if expiresSeconds > 1_000_000_000_000 {
		expiresSeconds = expiresAt / 1000
	}
	now := nowEpoch
	if now == 0 {
		now = time.Now().Unix()
	}
	return int64(expiresSeconds) <= now+oauthExpiryBufferSeconds
}

func claudeProfileDirs(home string) []string {
	root := filepath.Join(home, ".agent-switch", "profiles", "claude")
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var dirs []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(entry.Name()); err == nil {
			dirs = append(dirs, filepath.Join(root, entry.Name()))
		}
	}
	return dirs
}

func claudeCredentialsFromProfileDir(profileDir string) string {
	snapshot := readJSON(filepath.Join(profileDir, "keychain.json"))
	if secret := stringValue(snapshot["secret"]); secret != "" {
		return secret
	}
	for _, rel := range []string{filepath.Join("files", ".claude", ".credentials.json")} {
		data, err := os.ReadFile(filepath.Join(profileDir, rel))
		if err == nil {
			return string(data)
		}
	}
	return ""
}

func persistClaudeCredentials(profileDir, oldCredentials, newCredentials string) error {
	snapshotPath := filepath.Join(profileDir, "keychain.json")
	snapshot := readJSON(snapshotPath)
	if _, ok := snapshot["secret"]; !ok {
		return nil
	}
	snapshot["secret"] = newCredentials
	if err := writeJSONFile(snapshotPath, snapshot); err != nil {
		return err
	}
	return updateClaudeProfileFingerprint(profileDir, claudeKeychainFingerprint(oldCredentials), claudeKeychainFingerprint(newCredentials))
}

func updateClaudeProfileFingerprint(profileDir, oldFingerprint, newFingerprint string) error {
	manifestPath := filepath.Join(profileDir, "manifest.json")
	manifest := readJSON(manifestPath)
	current := stringValue(manifest["fingerprint"])
	if current != "" && current != oldFingerprint {
		return nil
	}
	manifest["fingerprint"] = newFingerprint
	return writeJSONFile(manifestPath, manifest)
}

func claudeKeychainFingerprint(credentials string) string {
	sum := sha256.Sum256([]byte("claude:" + claudeKeychainService + ":" + credentials))
	return hex.EncodeToString(sum[:])
}

func writeJSONFile(path string, payload any) error {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

func claudeOAuthData(credentials string) map[string]any {
	var data map[string]any
	if json.Unmarshal([]byte(credentials), &data) != nil {
		return nil
	}
	if oauth, ok := data["claudeAiOauth"].(map[string]any); ok {
		return oauth
	}
	if stringValue(data["accessToken"]) != "" || stringValue(data["refreshToken"]) != "" {
		return data
	}
	return nil
}

func rowFromClaudeWindow(label string, percentage any, resetsAt any, nowEpoch int64) map[string]any {
	pct, ok := numberValue(percentage)
	if !ok {
		return nil
	}
	row := map[string]any{
		"label":           label,
		"used_percentage": pct,
	}
	if clock, remaining, ok := resetStringsFromISO(stringValue(resetsAt), nowEpoch); ok {
		row["reset_at"] = clock
		row["remaining"] = remaining
	}
	return row
}

func resetStringsFromISO(value string, nowEpoch int64) (string, string, bool) {
	if strings.TrimSpace(value) == "" {
		return "", "", false
	}
	resetAt, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return "", "", false
	}
	now := time.Now()
	if nowEpoch != 0 {
		now = time.Unix(nowEpoch, 0)
	}
	return clockFromEpoch(float64(resetAt.Unix()), nowEpoch), remainingFromSeconds(resetAt.Sub(now).Seconds()), true
}

func errHTTPStatus(status int) error {
	return httpStatusError(status)
}

type httpStatusError int

func (e httpStatusError) Error() string {
	return "http status " + strconv.Itoa(int(e))
}

type CodexProvider struct {
	Fetcher               func(accessToken string) (map[string]any, error)
	RestrictToCurrentHome bool
	NowEpoch              int64
}

func (p CodexProvider) Load(home string) map[model.MetadataKey]model.Metadata {
	if p.RestrictToCurrentHome && !isCurrentHome(home) {
		return nil
	}
	auth := readJSON(filepath.Join(home, ".codex", "auth.json"))
	tokens, _ := auth["tokens"].(map[string]any)
	accessToken := stringValue(tokens["access_token"])
	if accessToken == "" {
		return nil
	}
	fallbackEmail := EmailFromCodexAuth(auth)
	oauthStatus := OAuthStatusFromCodexAuth(auth, p.NowEpoch)
	payload, err := p.fetch(accessToken)
	if err != nil {
		payload = map[string]any{}
	}
	metadata := MetadataByEmailFromCodexPayload(payload, fallbackEmail, p.NowEpoch)
	if oauthStatus != "" {
		if len(metadata) > 0 {
			for _, profileMetadata := range metadata {
				profileMetadata["oauth_status"] = oauthStatus
			}
		} else if fallbackEmail != "" {
			metadata[model.MetadataKey{Agent: "codex", Label: strings.ToLower(fallbackEmail)}] = model.Metadata{"oauth_status": oauthStatus}
		}
	}
	return metadata
}

func (p CodexProvider) fetch(accessToken string) (map[string]any, error) {
	if p.Fetcher != nil {
		return p.Fetcher(accessToken)
	}
	request, err := http.NewRequest(http.MethodGet, CodexUsageURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "agent-switch")
	client := http.Client{Timeout: 8 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func MetadataByEmailFromCodexPayload(payload map[string]any, fallbackEmail string, nowEpoch int64) map[model.MetadataKey]model.Metadata {
	email := stringValue(payload["email"])
	if email == "" {
		email = fallbackEmail
	}
	if email == "" {
		return map[model.MetadataKey]model.Metadata{}
	}
	usageRows := []any{}
	if rateLimit, ok := payload["rate_limit"].(map[string]any); ok {
		usageRows = append(usageRows, rowsFromRateLimit(rateLimit, "", nowEpoch)...)
	}
	if additional, ok := payload["additional_rate_limits"].([]any); ok {
		for _, item := range additional {
			limit, ok := item.(map[string]any)
			if !ok {
				continue
			}
			rateLimit, ok := limit["rate_limit"].(map[string]any)
			if !ok {
				continue
			}
			usageRows = append(usageRows, rowsFromRateLimit(rateLimit, shortLimitName(stringValue(limit["limit_name"])), nowEpoch)...)
		}
	}
	profileMetadata := model.Metadata{}
	if len(usageRows) > 0 {
		profileMetadata["usage_limits"] = usageRows
	}
	if plan := stringValue(payload["plan_type"]); plan != "" {
		profileMetadata["status_lines"] = []any{"plan: " + plan}
	}
	if len(profileMetadata) == 0 {
		return map[model.MetadataKey]model.Metadata{}
	}
	return map[model.MetadataKey]model.Metadata{
		{Agent: "codex", Label: strings.ToLower(email)}: profileMetadata,
	}
}

func OAuthStatusFromCodexAuth(auth map[string]any, nowEpoch int64) string {
	tokens, _ := auth["tokens"].(map[string]any)
	payload := decodeJWT(stringValue(tokens["access_token"]))
	exp, ok := numberValue(payload["exp"])
	if !ok {
		return ""
	}
	now := nowEpoch
	if now == 0 {
		now = time.Now().Unix()
	}
	state := "expired"
	if int64(exp) > now {
		state = "fresh"
	}
	refresh := "no"
	if stringValue(tokens["refresh_token"]) != "" {
		refresh = "yes"
	}
	remaining := remainingFromSeconds(exp - float64(now))
	if remaining != "" {
		return "oauth: " + state + ", refresh token " + refresh + ", expires " + clockFromEpoch(exp, now) + " " + remaining
	}
	return "oauth: " + state + ", refresh token " + refresh + ", expires " + clockFromEpoch(exp, now)
}

func EmailFromCodexAuth(auth map[string]any) string {
	tokens, _ := auth["tokens"].(map[string]any)
	for _, key := range []string{"id_token", "access_token"} {
		payload := decodeJWT(stringValue(tokens[key]))
		if email := stringValue(payload["email"]); email != "" {
			return email
		}
		if profile, ok := payload["https://api.openai.com/profile"].(map[string]any); ok {
			if email := stringValue(profile["email"]); email != "" {
				return email
			}
		}
	}
	return ""
}

func MergeProfilesWithUsageMetadata(profiles []model.Profile, metadata map[model.MetadataKey]model.Metadata) []model.Profile {
	merged := make([]model.Profile, 0, len(profiles))
	for _, profile := range profiles {
		next := profile
		next.Metadata = cloneMetadata(profile.Metadata)
		key := model.MetadataKey{Agent: profile.Agent, Label: strings.ToLower(profile.Label)}
		for itemKey, value := range metadata[key] {
			next.Metadata[itemKey] = value
		}
		merged = append(merged, next)
	}
	return merged
}

func rowsFromRateLimit(rateLimit map[string]any, prefix string, nowEpoch int64) []any {
	rows := []any{}
	for _, key := range []string{"primary_window", "secondary_window"} {
		window, ok := rateLimit[key].(map[string]any)
		if !ok {
			continue
		}
		label := windowLabel(window["limit_window_seconds"])
		if prefix != "" {
			label = prefix + " " + label
		}
		if row := rowFromWindow(label, window, nowEpoch); row != nil {
			rows = append(rows, row)
		}
	}
	return rows
}

func rowFromWindow(label string, window map[string]any, nowEpoch int64) map[string]any {
	used, ok := numberValue(window["used_percent"])
	if !ok {
		return nil
	}
	resetAfter, _ := numberValue(window["reset_after_seconds"])
	resetAt, ok := numberValue(window["reset_at"])
	clock := "?"
	if ok {
		clock = clockFromEpoch(resetAt, nowEpoch)
	}
	return map[string]any{
		"label":           label,
		"used_percentage": used,
		"reset_at":        clock,
		"remaining":       remainingFromSeconds(resetAfter),
	}
}

func usageLimitsFromCswapUsage(usage map[string]any, ageSeconds any) []any {
	rows := []any{}
	for _, pair := range []struct {
		key   string
		label string
	}{{"fiveHour", "5h"}, {"sevenDay", "7d"}} {
		if window, ok := usage[pair.key].(map[string]any); ok {
			if row := usageLimitRow(pair.label, window); row != nil {
				rows = append(rows, row)
			}
		}
	}
	if scoped, ok := usage["scoped"].([]any); ok {
		for _, item := range scoped {
			window, ok := item.(map[string]any)
			if !ok {
				continue
			}
			label := stringValue(window["name"])
			if label == "" {
				label = "scoped"
			}
			if row := usageLimitRow(label, window); row != nil {
				rows = append(rows, row)
			}
		}
	}
	if age := ageNote(ageSeconds); len(rows) > 0 && age != "" {
		rows[len(rows)-1].(map[string]any)["age"] = age
	}
	return rows
}

func usageLimitRow(label string, window map[string]any) map[string]any {
	pct, ok := numberValue(window["pct"])
	if !ok {
		return nil
	}
	countdown := stringValue(window["countdown"])
	return map[string]any{
		"label":           label,
		"used_percentage": pct,
		"reset_at":        stringValueOr(window["clock"], "?"),
		"remaining":       remainingText(countdown),
	}
}

func windowLabel(value any) string {
	seconds, ok := numberValue(value)
	if !ok {
		return "limit"
	}
	switch int(seconds) {
	case 18000:
		return "5h"
	case 604800:
		return "7d"
	}
	if int(seconds)%86400 == 0 {
		return strconvI(int(seconds)/86400) + "d"
	}
	if int(seconds)%3600 == 0 {
		return strconvI(int(seconds)/3600) + "h"
	}
	return "limit"
}

func shortLimitName(value string) string {
	if value == "" {
		return "limit"
	}
	if strings.Contains(value, "-Codex-") {
		parts := strings.Split(value, "-Codex-")
		return parts[len(parts)-1]
	}
	parts := strings.Split(value, "-")
	return parts[len(parts)-1]
}

func clockFromEpoch(epoch float64, nowEpoch int64) string {
	reset := time.Unix(int64(epoch), 0)
	now := time.Now()
	if nowEpoch != 0 {
		now = time.Unix(nowEpoch, 0)
	}
	if reset.Year() == now.Year() && reset.YearDay() == now.YearDay() {
		return reset.Format("15:04")
	}
	return reset.Format("Jan 2 15:04")
}

func remainingFromSeconds(seconds float64) string {
	total := int(seconds)
	if total < 0 {
		total = 0
	}
	days := total / 86400
	remainder := total % 86400
	hours := remainder / 3600
	minutes := (remainder % 3600) / 60
	if days > 0 {
		return "in " + strconvI(days) + "d " + strconvI(hours) + "h"
	}
	if hours > 0 {
		return "in " + strconvI(hours) + "h " + strconvI(minutes) + "m"
	}
	return "in " + strconvI(minutes) + "m"
}

func remainingText(countdown string) string {
	if countdown == "" {
		return ""
	}
	if strings.HasPrefix(countdown, "in ") {
		return countdown
	}
	return "in " + countdown
}

func ageNote(value any) string {
	seconds, ok := numberValue(value)
	if !ok || seconds <= 90 {
		return ""
	}
	total := int(seconds)
	if total < 3600 {
		return strconvI(total/60) + "m ago"
	}
	if total < 86400 {
		return strconvI(total/3600) + "h ago"
	}
	return strconvI(total/86400) + "d ago"
}

func decodeJWT(token string) map[string]any {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return map[string]any{}
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		payload, err = base64.URLEncoding.DecodeString(parts[1] + strings.Repeat("=", (4-len(parts[1])%4)%4))
		if err != nil {
			return map[string]any{}
		}
	}
	var out map[string]any
	if json.Unmarshal(payload, &out) != nil {
		return map[string]any{}
	}
	return out
}

func readJSON(path string) map[string]any {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if json.Unmarshal(data, &out) != nil {
		return map[string]any{}
	}
	return out
}

func mergeMetadata(dst, src map[model.MetadataKey]model.Metadata) {
	for key, metadata := range src {
		if dst[key] == nil {
			dst[key] = model.Metadata{}
		}
		for itemKey, value := range metadata {
			dst[key][itemKey] = value
		}
	}
}

func cloneMetadata(metadata model.Metadata) model.Metadata {
	out := model.Metadata{}
	for key, value := range metadata {
		out[key] = value
	}
	return out
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func stringValueOr(value any, fallback string) string {
	if text := stringValue(value); text != "" {
		return text
	}
	return fallback
}

func numberValue(value any) (float64, bool) {
	switch item := value.(type) {
	case float64:
		return item, true
	case float32:
		return float64(item), true
	case int:
		return float64(item), true
	case int64:
		return float64(item), true
	case json.Number:
		value, err := item.Float64()
		return value, err == nil
	default:
		return 0, false
	}
}

func isCurrentHome(home string) bool {
	current, err := os.UserHomeDir()
	return err == nil && filepath.Clean(home) == filepath.Clean(current)
}

func strconvI(value int) string {
	return strconv.Itoa(value)
}
