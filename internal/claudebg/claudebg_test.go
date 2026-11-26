package claudebg

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

type fakeCLI struct {
	calls     []string
	sessions  string
	listErr   error
	failingID string
}

func (f *fakeCLI) run(args ...string) ([]byte, error) {
	call := strings.Join(args, " ")
	f.calls = append(f.calls, call)
	switch {
	case call == "agents --json":
		return []byte(f.sessions), f.listErr
	case f.failingID != "" && call == "respawn "+f.failingID:
		return nil, errors.New("exit status 1")
	}
	return nil, nil
}

func TestFollowRestartsDaemonThenRespawnsEveryBackgroundSession(t *testing.T) {
	cli := &fakeCLI{sessions: `[
		{"id":"a1","kind":"background","status":"idle"},
		{"id":"b2","kind":"background","status":"busy"},
		{"id":"c3","kind":"interactive","status":"idle"},
		{"id":"d4","kind":"background","status":"idle"}
	]`}

	result, err := Follow(cli.run, "")

	if err != nil {
		t.Fatal(err)
	}
	wantCalls := []string{"agents --json", "daemon stop --any --keep-workers", "respawn a1", "respawn b2", "respawn d4"}
	if !reflect.DeepEqual(cli.calls, wantCalls) {
		t.Fatalf("calls = %#v", cli.calls)
	}
	if !reflect.DeepEqual(result.Moved, []string{"a1", "d4"}) || !reflect.DeepEqual(result.Interrupted, []string{"b2"}) || len(result.Failed) != 0 || result.Self != "" {
		t.Fatalf("result = %#v", result)
	}
}

func TestFollowLeavesItsOwnSessionForLast(t *testing.T) {
	cli := &fakeCLI{sessions: `[
		{"id":"a1","sessionId":"a1-full","kind":"background","status":"busy"},
		{"id":"e5","sessionId":"e5-full","kind":"background","status":"busy"}
	]`}

	result, err := Follow(cli.run, "e5-full")

	if err != nil {
		t.Fatal(err)
	}
	wantCalls := []string{"agents --json", "daemon stop --any --keep-workers", "respawn a1"}
	if !reflect.DeepEqual(cli.calls, wantCalls) {
		t.Fatalf("calls = %#v", cli.calls)
	}
	if result.Self != "e5" || !reflect.DeepEqual(result.Interrupted, []string{"a1"}) {
		t.Fatalf("result = %#v", result)
	}
}

func TestFollowReportsSessionsThatFailToRespawn(t *testing.T) {
	cli := &fakeCLI{failingID: "a1", sessions: `[
		{"id":"a1","kind":"background","status":"idle"},
		{"id":"d4","kind":"background","status":"idle"}
	]`}

	result, err := Follow(cli.run, "")

	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.Failed, []string{"a1"}) || !reflect.DeepEqual(result.Moved, []string{"d4"}) {
		t.Fatalf("result = %#v", result)
	}
}

func TestFollowLeavesDaemonAloneWhenSessionsCannotBeListed(t *testing.T) {
	cli := &fakeCLI{listErr: errors.New("claude not found")}

	if _, err := Follow(cli.run, ""); err == nil {
		t.Fatal("expected error")
	}
	if !reflect.DeepEqual(cli.calls, []string{"agents --json"}) {
		t.Fatalf("calls = %#v", cli.calls)
	}
}

func TestAccountEnvDropsCredentialOverrides(t *testing.T) {
	env := []string{
		"PATH=/usr/bin",
		"CLAUDE_CODE_OAUTH_TOKEN=token",
		"HOME=/Users/me",
		"ANTHROPIC_API_KEY=key",
		"ANTHROPIC_AUTH_TOKEN=auth",
		"ANTHROPIC_BASE_URL=https://gateway.example.com",
		"ANTHROPIC_CUSTOM_HEADERS=X-Key: secret",
		"CLAUDE_CODE_USE_BEDROCK=1",
		"CLAUDE_CONFIG_DIR=/tmp/other",
	}

	got := accountEnv(env)

	if !reflect.DeepEqual(got, []string{"PATH=/usr/bin", "HOME=/Users/me"}) {
		t.Fatalf("env = %#v", got)
	}
}
