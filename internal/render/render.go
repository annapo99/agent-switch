package render

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"

	"github.com/annapo99/agent-switch/internal/model"
)

const (
	reset  = "\033[0m"
	bold   = "\033[1m"
	dim    = "\033[2m"
	green  = "\033[32m"
	blue   = "\033[34m"
	cyan   = "\033[36m"
	yellow = "\033[33m"
	red    = "\033[31m"
)

func ShouldColor(stdout any) bool {
	switch strings.ToLower(os.Getenv("AGS_COLOR")) {
	case "always", "1", "true", "yes", "on":
		return true
	case "never", "0", "false", "no", "off":
		return false
	}
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	file, ok := stdout.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(file.Fd()))
}

func AgentHeading(name string, color bool) string {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "claude" || normalized == "codex" {
		return colorize(name, bold+blue, color)
	}
	return colorize(name, bold+cyan, color)
}

func ProfileTree(profile model.Profile, active bool, color bool) []string {
	head := fmt.Sprintf("  %d: %s", profile.Number, profile.Label)
	if org := profile.Metadata.String("organization_name"); org != "" {
		head += " [" + org + "]"
	}
	if active {
		head += " (active)"
	}
	lines := []string{colorProfileLine(head, color, active)}

	return append(lines, childLines(metadataChildren(profile.Metadata), color)...)
}

func SaveCandidateTree(candidate model.SaveCandidate, choiceNumber int, color bool) []string {
	account := model.ActiveAccount{
		Label:    candidate.Label,
		Metadata: candidate.Metadata,
	}
	return AccountSaveTree(account, choiceNumber, candidate.SaveNumber, candidate.DuplicateNumber, color)
}

func AccountSaveTree(account model.ActiveAccount, choiceNumber, saveNumber, duplicateNumber int, color bool) []string {
	prefix := ""
	if choiceNumber > 0 {
		prefix = fmt.Sprintf("%d) ", choiceNumber)
	}
	head := "  " + prefix + account.Label
	if org := account.Metadata.String("organization_name"); org != "" {
		head += " [" + org + "]"
	}
	action := "ready"
	if saveNumber > 0 {
		action = fmt.Sprintf("save as #%d", saveNumber)
	} else if duplicateNumber > 0 {
		action = fmt.Sprintf("already saved as #%d", duplicateNumber)
	}
	lines := []string{colorProfileLine(head, color, false)}
	children := append(metadataChildren(account.Metadata), child{kind: "action", text: action})
	return append(lines, childLines(children, color)...)
}

type child struct {
	kind  string
	value map[string]any
	text  string
}

func metadataChildren(metadata model.Metadata) []child {
	children := []child{}
	for _, usage := range usageLimits(metadata["usage_limits"]) {
		children = append(children, child{kind: "usage", value: usage})
	}
	if status := metadata.String("oauth_status"); status != "" {
		children = append(children, child{kind: "status", text: status})
	}
	statusLines := statusLines(metadata["status_lines"])
	if plan := planStatusLine(metadata); plan != "" && !hasPlanStatus(statusLines) {
		statusLines = append(statusLines, plan)
	}
	for _, status := range statusLines {
		children = append(children, child{kind: "status", text: status})
	}
	return children
}

func childLines(children []child, color bool) []string {
	var lines []string
	for index, item := range children {
		branch := "├"
		if index == len(children)-1 {
			branch = "└"
		}
		switch item.kind {
		case "usage":
			lines = append(lines, usageLine(branch, item.value, color))
		case "action":
			lines = append(lines, actionLine(branch, item.text, color))
		default:
			lines = append(lines, statusLine(branch, item.text, color))
		}
	}
	return lines
}

func usageLine(branch string, usage map[string]any, color bool) string {
	label := stringValue(usage["label"], "?")
	pct := percentValue(usage["used_percentage"])
	resetAt := stringValue(usage["reset_at"], "?")
	remaining := stringValue(usage["remaining"], "")
	age := stringValue(usage["age"], "")
	if age != "" {
		if remaining != "" {
			remaining += " · " + age
		} else {
			remaining = "· " + age
		}
	}
	labelText := fmt.Sprintf("%-8s", label)
	bar := progressBar(pct)
	var line string
	if resetAt == "?" && remaining == "" {
		line = fmt.Sprintf("     %s %s %s %3s%%", branch, labelText, bar, pct)
	} else {
		line = strings.TrimRight(fmt.Sprintf("     %s %s %s %3s%%   resets %-13s %s", branch, labelText, bar, pct, resetAt, remaining), " ")
	}
	return colorize(line, dim+yellow, color)
}

func progressBar(pct string) string {
	value, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(pct, "%")), 64)
	if err != nil {
		value = 0
	}
	value = math.Max(0, math.Min(100, value))
	filled := int(math.Round(value / 10))
	return strings.Repeat("█", filled) + strings.Repeat("░", 10-filled)
}

func statusLine(branch, status string, color bool) string {
	style := dim + yellow
	if isDangerStatus(status) {
		style = bold + red
	}
	return colorize(fmt.Sprintf("     %s • %s", branch, status), style, color)
}

func actionLine(branch, action string, color bool) string {
	return colorize(fmt.Sprintf("     %s %s", branch, action), dim+yellow, color)
}

func colorProfileLine(line string, color bool, active bool) string {
	if !color {
		return line
	}
	if active {
		return colorize(line, bold+green, true)
	}
	return colorize(line, cyan, true)
}

func colorize(text, code string, enabled bool) string {
	if !enabled {
		return text
	}
	return code + text + reset
}

func isDangerStatus(status string) bool {
	normalized := strings.ToLower(status)
	return strings.Contains(normalized, "oauth: expired") ||
		strings.Contains(normalized, "refresh token invalid")
}

func usageLimits(value any) []map[string]any {
	var rows []map[string]any
	switch items := value.(type) {
	case []any:
		for _, item := range items {
			if row, ok := item.(map[string]any); ok {
				rows = append(rows, row)
			}
		}
	case []map[string]any:
		return items
	}
	return rows
}

func statusLines(value any) []string {
	var lines []string
	switch items := value.(type) {
	case []any:
		for _, item := range items {
			if text, ok := item.(string); ok {
				lines = append(lines, text)
			}
		}
	case []string:
		lines = append(lines, items...)
	}
	return lines
}

func hasPlanStatus(lines []string) bool {
	for _, line := range lines {
		if strings.HasPrefix(line, "plan:") {
			return true
		}
	}
	return false
}

func planStatusLine(metadata model.Metadata) string {
	for _, key := range []string{"user_rate_limit_tier", "seat_tier", "organization_rate_limit_tier"} {
		if value := metadata.String(key); value != "" {
			return "plan: " + formatPlan(value)
		}
	}
	return ""
}

func formatPlan(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "default_")
	return strings.ReplaceAll(value, "_", " ")
}

func percentValue(value any) string {
	switch item := value.(type) {
	case int:
		return strconv.Itoa(item)
	case int64:
		return strconv.FormatInt(item, 10)
	case float64:
		if item == float64(int(item)) {
			return strconv.Itoa(int(item))
		}
		return strconv.FormatFloat(item, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(item), 'f', -1, 32)
	case string:
		return item
	default:
		return "?"
	}
}

func stringValue(value any, fallback string) string {
	if text, ok := value.(string); ok {
		return text
	}
	return fallback
}
