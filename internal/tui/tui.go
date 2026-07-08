package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/annapo99/agent-switch/internal/app"
	"github.com/annapo99/agent-switch/internal/model"
	"github.com/annapo99/agent-switch/internal/render"
)

const (
	menuIndexCurrent = iota
	menuIndexList
	menuIndexSave
	menuIndexUse
	menuIndexRemove
)

type commandResult struct {
	code   int
	output string
}

type commandRunner func(args []string) commandResult

type screen int

const (
	screenMenu screen = iota
	screenLoading
	screenOutput
	screenProfiles
	screenConfirmRemove
)

type profileAction int

const (
	actionSave profileAction = iota
	actionUse
	actionRemove
)

type menuItem struct {
	title       string
	description string
}

type commandFinishedMsg struct {
	title  string
	result commandResult
}

type profilesFinishedMsg struct {
	action profileAction
	result commandResult
}

type saveCandidatesFinishedMsg struct {
	result commandResult
}

type spinnerTickMsg struct{}

var menuItems = []menuItem{
	{title: "Current", description: "Show active accounts"},
	{title: "List", description: "Browse saved accounts"},
	{title: "Save", description: "Save detected accounts"},
	{title: "Use", description: "Switch account"},
	{title: "Remove", description: "Delete saved profile"},
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const logo = "" +
	"   ___                    __     _____          _ __       __\n" +
	"  / _ | ___ ____ ___  ___/ /_   / ___/    __ __(_) /______/ /\n" +
	" / __ |/ _ `/ -_) _ \\/ _  / /   \\__ \\ |/|/ / // __/ __/ _  /\n" +
	"/_/ |_|\\_, /\\__/_//_/\\_,_/_/   /____/__,__/_/\\__/\\__/\\_,_/\n" +
	"      /___/"

const repositoryURL = "https://github.com/annapo99/agent-switch"

type ansiStyle string

func (s ansiStyle) Render(text string) string {
	if text == "" {
		return text
	}
	return "\x1b[" + string(s) + "m" + text + "\x1b[0m"
}

var (
	logoStyle        = ansiStyle("1;38;2;88;166;255")
	titleStyle       = ansiStyle("1;38;2;88;166;255")
	subtitleStyle    = ansiStyle("38;2;139;148;158")
	cursorStyle      = ansiStyle("1;38;2;63;185;80")
	numberStyle      = ansiStyle("38;2;57;197;207")
	linkStyle        = ansiStyle("38;2;57;197;207")
	menuTitleStyle   = ansiStyle("1;38;2;230;237;243")
	descriptionStyle = ansiStyle("38;2;139;148;158")
	dangerStyle      = ansiStyle("1;38;2;255;107;107")
	shortcutStyle    = ansiStyle("1;38;2;210;153;34")
	outputStyle      = ansiStyle("38;2;230;237;243")
)

type uiModel struct {
	runner         commandRunner
	screen         screen
	cursor         int
	profileCursor  int
	profileAction  profileAction
	profiles       []model.Profile
	saveCandidates []model.SaveCandidate
	pending        model.Profile
	title          string
	output         string
	loadingDetail  string
	spinner        int
}

func Run(home string, stdin io.Reader, stdout, stderr io.Writer) int {
	program := tea.NewProgram(newModel(newServiceRunner(app.New(home))), tea.WithInput(stdin), tea.WithOutput(stdout), tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func newServiceRunner(service *app.Service) commandRunner {
	return func(args []string) commandResult {
		var out bytes.Buffer
		var errOut bytes.Buffer
		code := runWithForcedColor(func() int {
			return service.Run(args, strings.NewReader(""), &out, &errOut)
		})
		output := strings.TrimRight(out.String(), "\n")
		if errText := strings.TrimRight(errOut.String(), "\n"); errText != "" {
			if output != "" {
				output += "\n"
			}
			output += errText
		}
		return commandResult{code: code, output: output}
	}
}

func runWithForcedColor(run func() int) int {
	previous, hadPrevious := os.LookupEnv("AGS_COLOR")
	_ = os.Setenv("AGS_COLOR", "always")
	defer func() {
		if hadPrevious {
			_ = os.Setenv("AGS_COLOR", previous)
			return
		}
		_ = os.Unsetenv("AGS_COLOR")
	}()
	return run()
}

func newModel(runner commandRunner) uiModel {
	return uiModel{runner: runner}
}

func (m uiModel) Init() tea.Cmd {
	return nil
}

func (m uiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case commandFinishedMsg:
		return m.finishCommand(msg.title, msg.result), nil
	case profilesFinishedMsg:
		return m.finishProfiles(msg.action, msg.result), nil
	case saveCandidatesFinishedMsg:
		return m.finishSaveCandidates(msg.result), nil
	case spinnerTickMsg:
		if m.screen != screenLoading {
			return m, nil
		}
		m.spinner = (m.spinner + 1) % len(spinnerFrames)
		return m, spinnerTick()
	case tea.KeyMsg:
		return m.handleKey(msg)
	default:
		return m, nil
	}
}

func (m uiModel) handleKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "esc", "backspace", "b", "left", "h":
		return m.goBack(), nil
	case "up", "k":
		m.moveCursor(-1)
	case "down", "j":
		m.moveCursor(1)
	case "right", "l":
		return m.selectCurrent()
	case "r":
		if m.screen == screenProfiles {
			return m.startOpenProfiles(m.profileAction)
		}
	case "enter":
		return m.selectCurrent()
	}
	return m, nil
}

func (m uiModel) goBack() uiModel {
	if m.screen != screenMenu {
		m.screen = screenMenu
	}
	return m
}

func (m *uiModel) moveCursor(delta int) {
	switch m.screen {
	case screenProfiles:
		if m.selectionCount() == 0 {
			return
		}
		m.profileCursor = wrap(m.profileCursor+delta, m.selectionCount())
	default:
		m.cursor = wrap(m.cursor+delta, len(menuItems))
	}
}

func wrap(value, count int) int {
	if count <= 0 {
		return 0
	}
	if value < 0 {
		return count - 1
	}
	if value >= count {
		return 0
	}
	return value
}

func (m uiModel) selectCurrent() (tea.Model, tea.Cmd) {
	switch m.screen {
	case screenLoading:
		return m, nil
	case screenOutput:
		m.screen = screenMenu
		return m, nil
	case screenProfiles:
		return m.applyProfileAction()
	case screenConfirmRemove:
		return m.removePendingProfile()
	}

	switch m.cursor {
	case menuIndexCurrent:
		return m.startCommand("Current accounts", []string{"current"})
	case menuIndexList:
		return m.startCommand("Saved accounts", []string{"list"})
	case menuIndexSave:
		return m.startOpenSaveCandidates()
	case menuIndexUse:
		return m.startOpenProfiles(actionUse)
	case menuIndexRemove:
		return m.startOpenProfiles(actionRemove)
	default:
		return m, nil
	}
}

func (m uiModel) startCommand(title string, args []string) (uiModel, tea.Cmd) {
	m.screen = screenLoading
	m.title = title
	m.output = ""
	m.loadingDetail = "Running ags " + strings.Join(args, " ")
	m.spinner = 0
	return m, tea.Batch(func() tea.Msg {
		return commandFinishedMsg{title: title, result: m.runner(args)}
	}, spinnerTick())
}

func (m uiModel) finishCommand(title string, result commandResult) uiModel {
	m.screen = screenOutput
	m.title = title
	m.output = outputWithCode(result)
	m.loadingDetail = ""
	return m
}

func outputWithCode(result commandResult) string {
	output := strings.TrimSpace(result.output)
	if result.code != 0 {
		if output != "" {
			output += "\n"
		}
		output += fmt.Sprintf("exit code %d", result.code)
	}
	if output == "" {
		return "No output."
	}
	return output
}

func (m uiModel) startOpenProfiles(action profileAction) (uiModel, tea.Cmd) {
	title := "Choose account to use"
	if action == actionRemove {
		title = "Choose account to remove"
	}
	m.screen = screenLoading
	m.title = title
	m.output = ""
	m.loadingDetail = "Loading saved profiles"
	m.spinner = 0
	return m, tea.Batch(func() tea.Msg {
		return profilesFinishedMsg{action: action, result: m.runner([]string{"list", "--json"})}
	}, spinnerTick())
}

func (m uiModel) startOpenSaveCandidates() (uiModel, tea.Cmd) {
	m.screen = screenLoading
	m.title = "Choose account to save"
	m.output = ""
	m.loadingDetail = "Loading detected accounts"
	m.spinner = 0
	return m, tea.Batch(func() tea.Msg {
		return saveCandidatesFinishedMsg{result: m.runner([]string{"save", "--json"})}
	}, spinnerTick())
}

func (m uiModel) finishProfiles(action profileAction, result commandResult) uiModel {
	if result.code != 0 {
		m.screen = screenOutput
		m.title = "Saved profiles"
		m.output = outputWithCode(result)
		return m
	}
	var profiles []model.Profile
	if err := json.Unmarshal([]byte(result.output), &profiles); err != nil {
		m.screen = screenOutput
		m.title = "Saved profiles"
		m.output = "Failed to read saved profiles: " + err.Error()
		return m
	}
	if len(profiles) == 0 {
		m.screen = screenOutput
		m.title = "Saved profiles"
		m.output = "No saved accounts."
		return m
	}
	m.screen = screenProfiles
	m.profileAction = action
	m.profiles = profiles
	m.saveCandidates = nil
	m.profileCursor = 0
	m.loadingDetail = ""
	return m
}

func (m uiModel) finishSaveCandidates(result commandResult) uiModel {
	if result.code != 0 {
		m.screen = screenOutput
		m.title = "Detected accounts"
		m.output = outputWithCode(result)
		return m
	}
	var candidates []model.SaveCandidate
	if err := json.Unmarshal([]byte(result.output), &candidates); err != nil {
		m.screen = screenOutput
		m.title = "Detected accounts"
		m.output = "Failed to read detected accounts: " + err.Error()
		return m
	}
	if len(candidates) == 0 {
		m.screen = screenOutput
		m.title = "Detected accounts"
		m.output = "No active agent accounts detected."
		return m
	}
	m.screen = screenProfiles
	m.profileAction = actionSave
	m.profiles = nil
	m.saveCandidates = candidates
	m.profileCursor = 0
	m.loadingDetail = ""
	return m
}

func (m uiModel) applyProfileAction() (uiModel, tea.Cmd) {
	if m.selectionCount() == 0 {
		m.screen = screenMenu
		return m, nil
	}
	if m.profileAction == actionSave {
		candidate := m.saveCandidates[m.profileCursor]
		args := []string{"save", "--agent", candidate.Agent, "--yes"}
		return m.startCommand("Save account", args)
	}
	profile := m.profiles[m.profileCursor]
	args := []string{"use", strconv.Itoa(profile.Number), "--agent", profile.Agent, "--yes"}
	title := "Switch account"
	if m.profileAction == actionRemove {
		m.screen = screenConfirmRemove
		m.pending = profile
		return m, nil
	}
	return m.startCommand(title, args)
}

func (m uiModel) removePendingProfile() (uiModel, tea.Cmd) {
	args := []string{"remove", strconv.Itoa(m.pending.Number), "--agent", m.pending.Agent, "--yes"}
	return m.startCommand("Remove account", args)
}

func (m uiModel) View() string {
	switch m.screen {
	case screenLoading:
		return m.loadingView()
	case screenOutput:
		return m.outputView()
	case screenProfiles:
		return m.profilesView()
	case screenConfirmRemove:
		return m.confirmRemoveView()
	default:
		return m.menuView()
	}
}

func spinnerTick() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg {
		return spinnerTickMsg{}
	})
}

func (m uiModel) menuView() string {
	var b strings.Builder
	b.WriteString(logoStyle.Render(logo))
	b.WriteString("\n\n")
	b.WriteString(subtitleStyle.Render("Switch AI coding agent accounts"))
	b.WriteString("\n")
	b.WriteString(linkStyle.Render(repositoryURL))
	b.WriteString("\n\n")
	for index, item := range menuItems {
		cursor := " "
		if index == m.cursor {
			cursor = cursorStyle.Render("➤")
		}
		title := menuTitleStyle.Render(fmt.Sprintf("%-10s", item.title))
		if item.title == "Remove" {
			title = dangerStyle.Render(fmt.Sprintf("%-10s", item.title))
		}
		fmt.Fprintf(
			&b,
			"%s %s %s %s\n",
			cursor,
			numberStyle.Render(fmt.Sprintf("%d.", index+1)),
			title,
			descriptionStyle.Render(item.description),
		)
	}
	b.WriteString("\n")
	b.WriteString(shortcutStyle.Render("↑↓"))
	b.WriteString(descriptionStyle.Render(" Select  |  "))
	b.WriteString(shortcutStyle.Render("→/Enter"))
	b.WriteString(descriptionStyle.Render(" Open  |  "))
	b.WriteString(shortcutStyle.Render("←/B"))
	b.WriteString(descriptionStyle.Render(" Back  |  "))
	b.WriteString(shortcutStyle.Render("Q"))
	b.WriteString(descriptionStyle.Render(" Quit"))
	b.WriteString("\n")
	return b.String()
}

func (m uiModel) loadingView() string {
	frame := spinnerFrames[m.spinner%len(spinnerFrames)]
	var b strings.Builder
	b.WriteString(titleStyle.Render(frame + " Loading " + m.title))
	if m.loadingDetail != "" {
		b.WriteString("\n\n")
		b.WriteString(descriptionStyle.Render(m.loadingDetail))
	}
	b.WriteString("\n\n")
	b.WriteString(shortcutStyle.Render("Q"))
	b.WriteString(descriptionStyle.Render(" Quit"))
	b.WriteString("\n")
	return b.String()
}

func (m uiModel) outputView() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(m.title))
	b.WriteString("\n\n")
	b.WriteString(m.output)
	b.WriteString("\n\n←/B Back  |  Q Quit\n")
	return b.String()
}

func (m uiModel) profilesView() string {
	title := "Choose account to use"
	if m.profileAction == actionSave {
		title = "Choose account to save"
	} else if m.profileAction == actionRemove {
		title = "Choose account to remove"
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render(title))
	b.WriteString("\n\n")
	if m.profileAction == actionSave {
		m.writeSaveCandidateTrees(&b)
	} else {
		m.writeProfileTrees(&b)
	}
	b.WriteString("\n↑↓ Select  |  →/Enter Open  |  R Refresh  |  ←/B Back  |  Q Quit\n")
	return b.String()
}

func (m uiModel) writeProfileTrees(b *strings.Builder) {
	lastHeading := ""
	for index, profile := range m.profiles {
		if profile.DisplayName != lastHeading {
			if lastHeading != "" {
				b.WriteString("\n")
			}
			b.WriteString(render.AgentHeading(profile.DisplayName, true))
			b.WriteString("\n")
			lastHeading = profile.DisplayName
		}
		writeSelectableTree(b, index == m.profileCursor, render.ProfileTree(profile, profile.Active, true))
	}
}

func (m uiModel) writeSaveCandidateTrees(b *strings.Builder) {
	lastHeading := ""
	for index, candidate := range m.saveCandidates {
		if candidate.DisplayName != lastHeading {
			if lastHeading != "" {
				b.WriteString("\n")
			}
			b.WriteString(render.AgentHeading(candidate.DisplayName, true))
			b.WriteString("\n")
			lastHeading = candidate.DisplayName
		}
		writeSelectableTree(b, index == m.profileCursor, render.SaveCandidateTree(candidate, index+1, true))
	}
}

func writeSelectableTree(b *strings.Builder, selected bool, lines []string) {
	if len(lines) == 0 {
		return
	}
	cursor := " "
	if selected {
		cursor = cursorStyle.Render("➤")
	}
	fmt.Fprintf(b, "%s %s\n", cursor, lines[0])
	for _, line := range lines[1:] {
		fmt.Fprintf(b, "  %s\n", line)
	}
}

func (m uiModel) selectionCount() int {
	if m.profileAction == actionSave {
		return len(m.saveCandidates)
	}
	return len(m.profiles)
}

func (m uiModel) confirmRemoveView() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", dangerStyle.Render(fmt.Sprintf("Remove %s #%d?", m.pending.Agent, m.pending.Number)))
	b.WriteString(outputStyle.Render(m.pending.Label))
	b.WriteString("\n\n→/Enter Confirm  |  ←/B Back  |  Q Quit\n")
	return b.String()
}
