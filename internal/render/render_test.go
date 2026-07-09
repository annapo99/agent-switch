package render

import (
	"strings"
	"testing"

	"github.com/annapo99/agent-switch/internal/model"
)

func TestProfileTreeIncludesOrgUsageOAuthAndDerivedPlan(t *testing.T) {
	profile := model.Profile{
		Agent:       "claude",
		DisplayName: "Claude",
		Number:      3,
		Label:       "annapo.claude@example.com",
		Metadata: model.Metadata{
			"organization_name":    "Example Team",
			"user_rate_limit_tier": "default_claude_max_5x",
			"usage_limits": []any{
				map[string]any{"label": "5h", "used_percentage": float64(8), "reset_at": "19:39", "remaining": "in 4h 52m"},
				map[string]any{"label": "7d", "used_percentage": float64(2), "reset_at": "Jul 10 09:59", "remaining": "in 2d 19h"},
			},
			"oauth_status": "oauth: fresh, refresh token yes, expires 14:36 in 3h 7m",
		},
	}

	lines := ProfileTree(profile, true, false)

	want := []string{
		"  3: annapo.claude@example.com [Example Team] (active)",
		"     ├ 5h       █░░░░░░░░░   8%   resets 19:39         in 4h 52m",
		"     ├ 7d       ░░░░░░░░░░   2%   resets Jul 10 09:59  in 2d 19h",
		"     ├ • oauth: fresh, refresh token yes, expires 14:36 in 3h 7m",
		"     └ • plan: claude max 5x",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("lines:\n%s", strings.Join(lines, "\n"))
	}
}

func TestProfileTreeDoesNotDuplicateExplicitPlan(t *testing.T) {
	profile := model.Profile{
		Number: 3,
		Label:  "annapo.claude@example.com",
		Metadata: model.Metadata{
			"status_lines":         []any{"plan: pro"},
			"user_rate_limit_tier": "default_claude_max_5x",
		},
	}

	lines := ProfileTree(profile, false, false)

	if got := strings.Join(lines, "\n"); got != "  3: annapo.claude@example.com\n     └ • plan: pro" {
		t.Fatalf("lines:\n%s", got)
	}
}

func TestAccountSaveTreeUsesChoiceAndSaveNumber(t *testing.T) {
	account := model.ActiveAccount{
		Label:    "annapo.claude@example.com",
		Metadata: model.Metadata{"organization_name": "Example Team"},
	}

	lines := AccountSaveTree(account, 1, 3, 0, false)

	want := "  1) annapo.claude@example.com [Example Team]\n     └ save as #3"
	if got := strings.Join(lines, "\n"); got != want {
		t.Fatalf("lines:\n%s", got)
	}
}

func TestAgentHeadingUsesBlueForClaudeAndCodex(t *testing.T) {
	if got := AgentHeading("Claude", true); !strings.Contains(got, "\033[34m") {
		t.Fatalf("claude heading = %q", got)
	}
	if got := AgentHeading("Codex", true); !strings.Contains(got, "\033[34m") {
		t.Fatalf("codex heading = %q", got)
	}
}

func TestExpiredOAuthStatusUsesRedWhenColored(t *testing.T) {
	profile := model.Profile{
		Number: 1,
		Label:  "annapo.claude@example.com",
		Metadata: model.Metadata{
			"oauth_status": "oauth: expired, refresh token invalid, expires Jul 8 14:36 in 0m",
		},
	}

	lines := ProfileTree(profile, false, true)

	if !strings.Contains(lines[1], "\033[31m") {
		t.Fatalf("expired oauth should be red: %q", lines[1])
	}
}

func TestFreshOAuthStatusDoesNotUseRedWhenColored(t *testing.T) {
	profile := model.Profile{
		Number: 1,
		Label:  "annapo.claude@example.com",
		Metadata: model.Metadata{
			"oauth_status": "oauth: fresh, refresh token yes, expires 19:11 in 7h 59m",
		},
	}

	lines := ProfileTree(profile, false, true)

	if strings.Contains(lines[1], "\033[31m") {
		t.Fatalf("fresh oauth should not be red: %q", lines[1])
	}
}

func TestProgressBarRoundsAndClamps(t *testing.T) {
	tests := map[string]string{
		"14":  "█░░░░░░░░░",
		"22":  "██░░░░░░░░",
		"-10": "░░░░░░░░░░",
		"105": "██████████",
		"?":   "░░░░░░░░░░",
	}

	for input, want := range tests {
		if got := progressBar(input); got != want {
			t.Fatalf("progressBar(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestShouldColorCanBeForcedByEnvironment(t *testing.T) {
	t.Setenv("AGS_COLOR", "always")
	t.Setenv("NO_COLOR", "1")

	if !ShouldColor(nil) {
		t.Fatal("expected forced color")
	}
}

func TestShouldColorCanBeDisabledByEnvironment(t *testing.T) {
	t.Setenv("AGS_COLOR", "never")

	if ShouldColor(nil) {
		t.Fatal("expected color to be disabled")
	}
}

func TestShouldColorHonorsNoColorForAutomaticColor(t *testing.T) {
	t.Setenv("AGS_COLOR", "")
	t.Setenv("NO_COLOR", "1")

	if ShouldColor(nil) {
		t.Fatal("expected NO_COLOR to disable color")
	}
}
