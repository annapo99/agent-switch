// Package claudebg moves Claude Code background sessions onto the account
// currently stored in the keychain.
//
// The Claude Code background daemon keeps the credentials it started with and
// hands them to every background session it spawns, so switching the keychain
// account alone leaves background sessions on the previous account. Restarting
// the daemon (keeping its sessions alive) and respawning idle sessions makes
// them pick up the new account.
package claudebg

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

type Runner func(args ...string) ([]byte, error)

type Result struct {
	Moved  []string
	Busy   []string
	Failed []string
}

type session struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Status string `json:"status"`
}

// Follow restarts the background daemon and respawns idle background sessions
// so they run on the current account. Busy sessions keep running on the
// previous account rather than losing the work in progress.
func Follow(run Runner) (Result, error) {
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
		if item.Status == "busy" {
			result.Busy = append(result.Busy, item.ID)
			continue
		}
		if _, err := run("respawn", item.ID); err != nil {
			result.Failed = append(result.Failed, item.ID)
			continue
		}
		result.Moved = append(result.Moved, item.ID)
	}
	return result, nil
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
	"CLAUDE_CODE_OAUTH_TOKEN": true,
	"ANTHROPIC_API_KEY":       true,
	"ANTHROPIC_AUTH_TOKEN":    true,
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
