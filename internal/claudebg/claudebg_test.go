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

func TestFollowRestartsDaemonThenRespawnsIdleBackgroundSessions(t *testing.T) {
	cli := &fakeCLI{sessions: `[
		{"id":"a1","kind":"background","status":"idle"},
		{"id":"b2","kind":"background","status":"busy"},
		{"id":"c3","kind":"interactive","status":"idle"},
		{"id":"d4","kind":"background","status":"idle"}
	]`}

	result, err := Follow(cli.run)

	if err != nil {
		t.Fatal(err)
	}
	wantCalls := []string{"agents --json", "daemon stop --any --keep-workers", "respawn a1", "respawn d4"}
	if !reflect.DeepEqual(cli.calls, wantCalls) {
		t.Fatalf("calls = %#v", cli.calls)
	}
	if !reflect.DeepEqual(result.Moved, []string{"a1", "d4"}) || !reflect.DeepEqual(result.Busy, []string{"b2"}) || len(result.Failed) != 0 {
		t.Fatalf("result = %#v", result)
	}
}

func TestFollowReportsSessionsThatFailToRespawn(t *testing.T) {
	cli := &fakeCLI{failingID: "a1", sessions: `[
		{"id":"a1","kind":"background","status":"idle"},
		{"id":"d4","kind":"background","status":"idle"}
	]`}

	result, err := Follow(cli.run)

	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.Failed, []string{"a1"}) || !reflect.DeepEqual(result.Moved, []string{"d4"}) {
		t.Fatalf("result = %#v", result)
	}
}

func TestFollowLeavesDaemonAloneWhenSessionsCannotBeListed(t *testing.T) {
	cli := &fakeCLI{listErr: errors.New("claude not found")}

	if _, err := Follow(cli.run); err == nil {
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
	}

	got := accountEnv(env)

	if !reflect.DeepEqual(got, []string{"PATH=/usr/bin", "HOME=/Users/me"}) {
		t.Fatalf("env = %#v", got)
	}
}
