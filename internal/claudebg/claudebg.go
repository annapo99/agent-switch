// Package claudebg moves Claude Code background sessions onto the account
// currently stored in the keychain.
//
// The Claude Code background daemon keeps the credentials it started with and
// hands them to every background session it spawns, so switching the keychain
// account alone leaves background sessions on the previous account. Restarting
// the daemon (keeping its sessions alive) and respawning every session makes
// them pick up the new account.
package claudebg

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

type Runner func(args ...string) ([]byte, error)

type Result struct {
	Moved []string
	// Interrupted sessions were busy and got restarted mid-task.
	Interrupted []string
	Failed      []string
	// Self is the session ags is running in. Respawning it kills ags, so it
	// is left for RespawnDetached once everything else has moved.
	Self string
}

type session struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	Kind      string `json:"kind"`
	Status    string `json:"status"`
}

// Follow restarts the background daemon and respawns every background session
// so it runs on the current account, busy ones included: their in-progress
// work is cut off. self is the CLAUDE_CODE_SESSION_ID ags runs under, if any.
func Follow(run Runner, self string) (Result, error) {
	out, err := run("agents", "--json")
	if err != nil {
		return Result{}, fmt.Errorf("list background sessions: %w", err)
	}
	var sessions []session
	if err := json.Unmarshal(out, &sessions); err != nil {
		return Result{}, fmt.Errorf("read background sessions: %w", err)
	}
	if _, err := run("daemon", "stop", "--any", "--keep-workers"); err != nil {
		return Result{}, fmt.Errorf("restart background daemon: %w", err)
	}
	var result Result
	for _, item := range sessions {
		if item.Kind != "background" {
			continue
		}
		if self != "" && item.SessionID == self {
			result.Self = item.ID
			continue
		}
		if _, err := run("respawn", item.ID); err != nil {
			result.Failed = append(result.Failed, item.ID)
			continue
		}
		if item.Status == "busy" {
			result.Interrupted = append(result.Interrupted, item.ID)
			continue
		}
		result.Moved = append(result.Moved, item.ID)
	}
	return result, nil
}

// RespawnDetached respawns the session ags is running in after a short delay,
// from its own process session so the restart that kills ags does not take
// the respawn down with it. The delay lets ags finish and its output land.
func RespawnDetached(id string) error {
	path, err := exec.LookPath("claude")
	if err != nil {
		return err
	}
	cmd := exec.Command("/bin/sh", "-c", `sleep 2 && exec "$0" respawn "$1"`, path, id)
	cmd.Env = accountEnv(os.Environ())
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// CLI runs the installed claude command. Credential overrides are removed from
// its environment so the restarted daemon reads the keychain account.
func CLI(args ...string) ([]byte, error) {
	path, err := exec.LookPath("claude")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = accountEnv(os.Environ())
	return cmd.Output()
}

var credentialOverrides = map[string]bool{
	"CLAUDE_CODE_OAUTH_TOKEN":  true,
	"ANTHROPIC_API_KEY":        true,
	"ANTHROPIC_AUTH_TOKEN":     true,
	"ANTHROPIC_BASE_URL":       true,
	"ANTHROPIC_CUSTOM_HEADERS": true,
	"ANTHROPIC_UNIX_SOCKET":    true,
	"CLAUDE_CODE_USE_BEDROCK":  true,
	"CLAUDE_CODE_USE_VERTEX":   true,
	"CLAUDE_CODE_USE_FOUNDRY":  true,
	"CLAUDE_CONFIG_DIR":        true,
}

func accountEnv(env []string) []string {
	var kept []string
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		if !credentialOverrides[name] {
			kept = append(kept, entry)
		}
	}
	return kept
}
