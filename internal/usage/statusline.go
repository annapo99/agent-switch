package usage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const statusLineSessionsFile = "statusline-sessions.json"
const statusLineSessionLifetime = 7 * 24 * 60 * 60

type StatusLine struct {
	SessionID     string
	APIDurationMS float64
	Usage         map[string]any
}

type statusLineSession struct {
	APIDurationMS float64 `json:"api_duration_ms"`
	TokenID       string  `json:"token_id"`
	SeenAt        int64   `json:"seen_at"`
}

func ParseStatusLine(input []byte) (StatusLine, bool) {
	var status struct {
		SessionID  string                     `json:"session_id"`
		RateLimits map[string]json.RawMessage `json:"rate_limits"`
		Cost       struct {
			APIDurationMS float64 `json:"total_api_duration_ms"`
		} `json:"cost"`
	}
	if json.Unmarshal(input, &status) != nil || status.SessionID == "" {
		return StatusLine{}, false
	}
	payload := map[string]any{}
	for _, key := range []string{"five_hour", "seven_day"} {
		var window struct {
			UsedPercentage *float64 `json:"used_percentage"`
			ResetsAt       *float64 `json:"resets_at"`
		}
		if json.Unmarshal(status.RateLimits[key], &window) != nil || window.UsedPercentage == nil {
			continue
		}
		entry := map[string]any{"utilization": *window.UsedPercentage}
		if window.ResetsAt != nil {
			entry["resets_at"] = time.Unix(int64(*window.ResetsAt), 0).UTC().Format(time.RFC3339)
		}
		payload[key] = entry
	}
	if len(payload) == 0 {
		return StatusLine{}, false
	}
	return StatusLine{SessionID: status.SessionID, APIDurationMS: status.Cost.APIDurationMS, Usage: payload}, true
}

func ObserveStatusLine(home string, line StatusLine, activeTokenID func() string, now int64) (string, error) {
	path := filepath.Join(home, ".agent-switch", statusLineSessionsFile)
	sessions := map[string]statusLineSession{}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &sessions)
	}
	previous, seen := sessions[line.SessionID]
	if seen && line.APIDurationMS <= previous.APIDurationMS {
		return "", nil
	}
	tokenID := activeTokenID()
	sessions[line.SessionID] = statusLineSession{APIDurationMS: line.APIDurationMS, TokenID: tokenID, SeenAt: now}
	for id, session := range sessions {
		if now-session.SeenAt > statusLineSessionLifetime {
			delete(sessions, id)
		}
	}
	if err := writeJSONFile(path, sessions); err != nil {
		return "", err
	}
	if seen && tokenID != "" && previous.TokenID == tokenID {
		return tokenID, nil
	}
	return "", nil
}

func SaveClaudeUsage(profileDir string, payload map[string]any, now int64) error {
	return writeJSONFile(filepath.Join(profileDir, claudeUsageCacheFile), map[string]any{"fetched_at": now, "usage": payload})
}
