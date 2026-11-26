package app

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/annapo99/agent-switch/internal/claudebg"
	"github.com/annapo99/agent-switch/internal/identity"
	"github.com/annapo99/agent-switch/internal/model"
	"github.com/annapo99/agent-switch/internal/provider"
	"github.com/annapo99/agent-switch/internal/render"
	"github.com/annapo99/agent-switch/internal/store"
	"github.com/annapo99/agent-switch/internal/update"
	"github.com/annapo99/agent-switch/internal/usage"
)

type Service struct {
	home          string
	version       string
	providers     []provider.Provider
	usageProvider usage.Provider
	store         *store.Store
	// claudeBackground moves Claude Code background sessions onto the
	// account that was just switched in. Nil for any home other than the
	// current user's, so tests never restart the real daemon.
	claudeBackground func() (claudebg.Result, error)
	// respawnClaudeSelf restarts the session ags runs in once the rest have
	// moved. Nil wherever claudeBackground is.
	respawnClaudeSelf func(id string) error
	claudeIdentity    func(accessToken string) (provider.ClaudeLoginIdentity, error)
}

var shouldColor = func(stdout any) bool {
	return render.ShouldColor(stdout)
}

var runUpdater = func(home, version string, stdout, stderr io.Writer) int {
	result, err := update.Update(context.Background(), update.Config{Home: home, CurrentVersion: version})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if result.Updated {
		fmt.Fprintf(stdout, "Updated ags to %s\n", result.Version)
		return 0
	}
	fmt.Fprintln(stdout, "ags is up to date")
	return 0
}

type options struct {
	agent string
	yes   bool
	json  bool
}

func New(home string) *Service {
	return NewWithVersion(home, "dev")
}

func NewWithVersion(home, version string) *Service {
	return NewWithUsageProviderAndVersion(home, usage.DefaultProvider(), version)
}

func NewWithUsageProvider(home string, usageProvider usage.Provider) *Service {
	return NewWithUsageProviderAndVersion(home, usageProvider, "dev")
}

func NewWithUsageProviderAndVersion(home string, usageProvider usage.Provider, version string) *Service {
	service := &Service{
		home:          home,
		version:       version,
		providers:     provider.DefaultProviders(),
		usageProvider: usageProvider,
		store:         store.New(home),
	}
	if isCurrentUserHome(home) {
		service.claudeBackground = func() (claudebg.Result, error) {
			return claudebg.Follow(claudebg.CLI, os.Getenv("CLAUDE_CODE_SESSION_ID"))
		}
		service.respawnClaudeSelf = claudebg.RespawnDetached
		service.claudeIdentity = provider.FetchClaudeLoginIdentity
	}
	return service
}

func isCurrentUserHome(home string) bool {
	current, err := os.UserHomeDir()
	return err == nil && filepath.Clean(home) == filepath.Clean(current)
}

var commandLockWait = time.Minute

func (s *Service) Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		printHelp(stdout)
		return 0
	}
	command := args[0]
	if command != "update" {
		wait := commandLockWait
		if command == "record-usage" {
			wait = 0
		}
		unlock, err := s.store.Lock(wait)
		if err != nil {
			if command == "record-usage" {
				return 0
			}
			fmt.Fprintln(stderr, err)
			return 1
		}
		defer unlock()
	}
	switch command {
	case "save":
		if hasArg(args[1:], "--token") {
			label, ok := parseTokenOptions(args[1:], stderr)
			if !ok {
				return 2
			}
			return s.SaveToken(stdout, stderr, stdin, label)
		}
		opts, ok := parseOptions(args[1:], true, stdout, stderr)
		if !ok {
			return 2
		}
		if opts.json {
			writeJSON(stdout, s.SaveCandidates(opts.agent))
			return 0
		}
		return s.Save(stdout, stdin, opts.agent, opts.yes)
	case "use":
		number, rest, ok := parseNumber(args[1:], stderr)
		if !ok {
			return 2
		}
		opts, ok := parseOptions(rest, false, stdout, stderr)
		if !ok {
			return 2
		}
		return s.Use(number, stdout, stdin, opts.agent, opts.yes)
	case "list":
		opts, ok := parseOptions(args[1:], true, stdout, stderr)
		if !ok {
			return 2
		}
		return s.ListJSONAware(stdout, opts.agent, opts.json)
	case "current":
		opts, ok := parseOptions(args[1:], true, stdout, stderr)
		if !ok {
			return 2
		}
		return s.ListJSONAware(stdout, opts.agent, opts.json)
	case "remove":
		number, rest, ok := parseNumber(args[1:], stderr)
		if !ok {
			return 2
		}
		opts, ok := parseOptions(rest, false, stdout, stderr)
		if !ok {
			return 2
		}
		return s.Remove(number, stdout, stdin, opts.agent, opts.yes)
	case "rename":
		number, rest, ok := parseNumber(args[1:], stderr)
		if !ok {
			return 2
		}
		if len(rest) == 0 || strings.TrimSpace(rest[0]) == "" || strings.HasPrefix(rest[0], "-") {
			fmt.Fprintln(stderr, "missing name")
			return 2
		}
		opts, ok := parseOptions(rest[1:], false, stdout, stderr)
		if !ok {
			return 2
		}
		return s.Rename(number, strings.TrimSpace(rest[0]), stdout, stdin, opts.agent, opts.yes)
	case "update":
		return runUpdater(s.home, s.version, stdout, stderr)
	case "record-usage":
		return s.RecordUsage(stdin, stderr)
	default:
		fmt.Fprintf(stderr, "Unknown command: %s\n", command)
		return 2
	}
}

func (s *Service) Save(stdout io.Writer, stdin io.Reader, agent string, yes bool) int {
	detected := s.detectedAccounts(agent)
	if len(detected) == 0 {
		fmt.Fprintln(stdout, "No active agent accounts detected.")
		return 1
	}

	type newItem struct {
		provider provider.Provider
		account  model.ActiveAccount
		number   int
	}
	type duplicateItem struct {
		provider  provider.Provider
		account   model.ActiveAccount
		duplicate model.Profile
	}
	var newItems []newItem
	var duplicates []duplicateItem
	for _, item := range detected {
		if duplicate, ok := s.store.FindDuplicate(item.account); ok {
			duplicates = append(duplicates, duplicateItem{provider: item.provider, account: item.account, duplicate: duplicate})
		} else {
			newItems = append(newItems, newItem{provider: item.provider, account: item.account, number: s.store.NextNumber(item.account.Agent)})
		}
	}

	if len(newItems) == 0 {
		color := shouldColor(stdout)
		updated := false
		for _, item := range duplicates {
			fmt.Fprintf(stdout, "Detected %s account\n\n", item.provider.DisplayName())
			for _, line := range render.AccountSaveTree(item.account, 0, 0, item.duplicate.Number, color) {
				fmt.Fprintln(stdout, line)
			}
			fmt.Fprintln(stdout)
			if s.needsUpdate(item.account, item.duplicate) {
				if err := s.verifyLogin(detectedAccount{provider: item.provider, account: item.account}); err != nil {
					fmt.Fprintln(stdout, err)
					return 1
				}
				profile, removed, err := s.updateAccount(item.provider, item.account, item.duplicate)
				if err != nil {
					fmt.Fprintln(stdout, err)
					return 1
				}
				fmt.Fprintf(stdout, "Updated %s account #%d\n", profile.Agent, profile.Number)
				for _, removedProfile := range removed {
					fmt.Fprintf(stdout, "Removed duplicate %s account #%d\n", removedProfile.Agent, removedProfile.Number)
				}
				updated = true
			}
		}
		if !updated {
			fmt.Fprintln(stdout, "Nothing to save.")
		}
		return 0
	}

	for _, item := range newItems {
		if err := s.verifyLogin(detectedAccount{provider: item.provider, account: item.account}); err != nil {
			fmt.Fprintln(stdout, err)
			return 1
		}
	}
	reader := bufio.NewReader(stdin)
	if len(newItems) == 1 {
		item := newItems[0]
		if !yes {
			color := shouldColor(stdout)
			fmt.Fprintf(stdout, "Detected %s account\n\n", item.provider.DisplayName())
			for _, line := range render.AccountSaveTree(item.account, 0, item.number, 0, color) {
				fmt.Fprintln(stdout, line)
			}
			fmt.Fprintln(stdout)
			if !confirmDefaultYes(stdout, reader, "Save this account? [Y/n] ") {
				fmt.Fprintln(stdout, "Canceled.")
				return 1
			}
		}
		profile, err := s.saveAccount(item.provider, item.account)
		if err != nil {
			fmt.Fprintln(stdout, err)
			return 1
		}
		fmt.Fprintf(stdout, "Saved %s account #%d\n", profile.Agent, profile.Number)
		return 0
	}

	if yes {
		for _, item := range newItems {
			profile, err := s.saveAccount(item.provider, item.account)
			if err != nil {
				fmt.Fprintln(stdout, err)
				return 1
			}
			fmt.Fprintf(stdout, "Saved %s account #%d\n", profile.Agent, profile.Number)
		}
		return 0
	}

	fmt.Fprintln(stdout, "Detected active agent accounts")
	fmt.Fprintln(stdout)
	color := shouldColor(stdout)
	grouped := map[string][]newItem{}
	order := []string{}
	for _, item := range newItems {
		name := item.provider.DisplayName()
		if _, ok := grouped[name]; !ok {
			order = append(order, name)
		}
		grouped[name] = append(grouped[name], item)
	}
	choiceIndex := 1
	choiceMap := map[int]newItem{}
	for _, name := range order {
		fmt.Fprintln(stdout, render.AgentHeading(name, color))
		for _, item := range grouped[name] {
			choiceMap[choiceIndex] = item
			for _, line := range render.AccountSaveTree(item.account, choiceIndex, item.number, 0, color) {
				fmt.Fprintln(stdout, line)
			}
			choiceIndex++
		}
	}
	fmt.Fprintln(stdout)
	selected := selectOrAll(stdout, reader, "Which account should be saved? ", len(newItems))
	if selected == nil {
		fmt.Fprintln(stdout, "Canceled.")
		return 1
	}
	for _, index := range selected {
		item := choiceMap[index+1]
		profile, err := s.saveAccount(item.provider, item.account)
		if err != nil {
			fmt.Fprintln(stdout, err)
			return 1
		}
		fmt.Fprintf(stdout, "Saved %s account #%d\n", profile.Agent, profile.Number)
	}
	return 0
}

// SaveToken saves a long-lived Claude token from `claude setup-token`, read
// from stdin, as a Claude profile named label. Saving the same label again
// replaces that profile's token.
func (s *Service) SaveToken(stdout, stderr io.Writer, stdin io.Reader, label string) int {
	token := readToken(stdin, stderr)
	if !strings.HasPrefix(token, claudeTokenPrefix) || strings.ContainsAny(token, " \t") {
		fmt.Fprintln(stderr, "Pass the token printed by `claude setup-token` on stdin.")
		return 2
	}
	claude, ok := s.providerByAgent("claude").(*provider.ClaudeProvider)
	if !ok {
		fmt.Fprintln(stderr, "Claude is not supported here.")
		return 1
	}
	account, secret := claude.TokenAccount(s.home, label, token, time.Now())
	verb := "Saved"
	var profile model.Profile
	var err error
	if existing, found := s.tokenProfile(account); found {
		verb = "Updated"
		err = claude.SaveTokenSnapshot(s.home, label, secret, s.store.ProfileDir(existing.Agent, existing.Number))
		if err == nil {
			profile, err = s.store.UpdateProfile(existing, account)
		}
	} else if profile, err = s.store.CreateProfile(account); err == nil {
		if err = claude.SaveTokenSnapshot(s.home, label, secret, s.store.ProfileDir(profile.Agent, profile.Number)); err != nil {
			s.store.RemoveProfile(profile.Agent, profile.Number)
		}
	}
	if err != nil {
		fmt.Fprintln(stdout, err)
		return 1
	}
	fmt.Fprintf(stdout, "%s claude account #%d (token: %s)\n", verb, profile.Number, label)
	return 0
}

const claudeTokenPrefix = "sk-ant-oat"

func readToken(stdin io.Reader, stderr io.Writer) string {
	file, ok := stdin.(*os.File)
	if !ok || !term.IsTerminal(int(file.Fd())) {
		line, _ := bufio.NewReader(stdin).ReadString('\n')
		return strings.TrimSpace(line)
	}
	fd := int(file.Fd())
	if state, err := term.GetState(fd); err == nil {
		interrupted := make(chan os.Signal, 1)
		signal.Notify(interrupted, os.Interrupt)
		defer signal.Stop(interrupted)
		go func() {
			<-interrupted
			_ = term.Restore(fd, state)
			os.Exit(130)
		}()
	}
	fmt.Fprint(stderr, "Token: ")
	data, _ := term.ReadPassword(fd)
	fmt.Fprintln(stderr)
	return strings.TrimSpace(string(data))
}

// tokenProfile finds the saved token profile a new token replaces.
func (s *Service) tokenProfile(account model.ActiveAccount) (model.Profile, bool) {
	for _, profile := range s.store.ListProfiles(account.Agent) {
		if profile.Metadata.String(model.MetadataCredentialKind) != identity.CredentialKindToken {
			continue
		}
		if identity.SameLabel(profile.Label, account.Label) || identity.SameLogicalAccount(profile, account) {
			return profile, true
		}
	}
	return model.Profile{}, false
}

func (s *Service) providerByAgent(agent string) provider.Provider {
	for _, item := range s.providers {
		if item.Agent() == agent {
			return item
		}
	}
	return nil
}

func (s *Service) Use(number int, stdout io.Writer, stdin io.Reader, agent string, yes bool) int {
	matches := s.store.ProfilesByNumber(number, agent)
	if len(matches) == 0 {
		fmt.Fprintf(stdout, "No saved account matches #%d.\n", number)
		return 1
	}
	profile, ok := s.resolveProfile(matches, stdout, stdin, "Which account should be used? ", yes)
	if !ok {
		return 1
	}
	provider := s.providerByAgent(profile.Agent)
	if provider == nil {
		fmt.Fprintf(stdout, "Unsupported agent: %s\n", profile.Agent)
		return 1
	}
	if expecter, ok := provider.(interface{ ExpectCurrentLogin(home string) }); ok {
		expecter.ExpectCurrentLogin(s.home)
	}
	s.syncActiveProfiles(profile.Agent)
	current := s.store.ProfilesByNumber(profile.Number, profile.Agent)
	if len(current) == 0 {
		fmt.Fprintf(stdout, "No saved account matches #%d.\n", number)
		return 1
	}
	if provider.MatchesProfile(s.home, current[0]) {
		fmt.Fprintf(stdout, "Already using %s account #%d.\n", profile.Agent, profile.Number)
		return 0
	}
	if err := provider.ApplySnapshot(s.home, s.store.ProfileDir(profile.Agent, profile.Number)); err != nil {
		fmt.Fprintln(stdout, err)
		return 1
	}
	if noticer, ok := provider.(interface{ TakeNotices() []string }); ok {
		for _, notice := range noticer.TakeNotices() {
			fmt.Fprintln(stdout, notice)
		}
	}
	name := profile.DisplayName
	if name == "" {
		name = profile.Agent
	}
	if profile.Agent == "claude" {
		fmt.Fprintf(stdout, "Switched %s to account #%d. Running sessions may take up to ~30s to pick it up.\n", name, profile.Number)
		s.moveClaudeBackground(stdout)
		return 0
	}
	fmt.Fprintf(stdout, "Switched %s to account #%d\n", name, profile.Number)
	return 0
}

func (s *Service) moveClaudeBackground(stdout io.Writer) {
	if s.claudeBackground == nil {
		return
	}
	result, err := s.claudeBackground()
	if err != nil {
		fmt.Fprintf(stdout, "Background sessions were not moved: %v\n", err)
		return
	}
	if count := len(result.Moved); count > 0 {
		fmt.Fprintf(stdout, "Moved %d background %s to this account.\n", count, plural(count, "session", "sessions"))
	}
	if count := len(result.Interrupted); count > 0 {
		fmt.Fprintf(stdout, "Restarted %d busy background %s on this account; in-progress work was cut off.\n",
			count, plural(count, "session", "sessions"))
	}
	if len(result.Failed) > 0 {
		fmt.Fprintf(stdout, "Could not move background sessions: %s\n", strings.Join(result.Failed, ", "))
	}
	if result.Self == "" || s.respawnClaudeSelf == nil {
		return
	}
	if err := s.respawnClaudeSelf(result.Self); err != nil {
		fmt.Fprintf(stdout, "Could not move this session: %v\n", err)
		return
	}
	fmt.Fprintln(stdout, "This session restarts on this account in a few seconds.")
}

func plural(count int, one, many string) string {
	if count == 1 {
		return one
	}
	return many
}

func (s *Service) List(stdout io.Writer, agent string, jsonOutput bool) int {
	return s.ListJSONAware(stdout, agent, jsonOutput)
}

func (s *Service) Current(stdout io.Writer, agent string) int {
	return s.ListJSONAware(stdout, agent, false)
}

func (s *Service) ListJSONAware(stdout io.Writer, agent string, jsonOutput bool) int {
	s.syncActiveProfiles(agent)
	current := s.withUsageMetadata(s.store.ListProfiles(agent))
	s.markActiveProfiles(current)
	if jsonOutput {
		writeJSON(stdout, current)
		return 0
	}
	if len(current) == 0 {
		fmt.Fprintln(stdout, "No saved accounts.")
		return 0
	}
	color := shouldColor(stdout)
	fmt.Fprintln(stdout, "List accounts")
	fmt.Fprintln(stdout)
	for _, group := range groupProfiles(current) {
		fmt.Fprintln(stdout, render.AgentHeading(group.name, color))
		for _, profile := range group.profiles {
			for _, line := range render.ProfileTree(profile, profile.Active, color) {
				fmt.Fprintln(stdout, line)
			}
		}
		fmt.Fprintln(stdout)
	}
	return 0
}

func (s *Service) Remove(number int, stdout io.Writer, stdin io.Reader, agent string, yes bool) int {
	matches := s.store.ProfilesByNumber(number, agent)
	if len(matches) == 0 {
		fmt.Fprintf(stdout, "No saved account matches #%d.\n", number)
		return 1
	}
	profile, ok := s.resolveProfile(matches, stdout, stdin, "Which account should be removed? ", yes)
	if !ok {
		return 1
	}
	if len(matches) == 1 && !yes {
		reader := bufio.NewReader(stdin)
		if !confirm(stdout, reader, fmt.Sprintf("Remove %s account #%d? [y/N, Enter cancels] ", profile.Agent, profile.Number)) {
			fmt.Fprintln(stdout, "Canceled.")
			return 1
		}
	}
	if !s.store.RemoveProfile(profile.Agent, profile.Number) {
		fmt.Fprintf(stdout, "No saved account matches #%d.\n", number)
		return 1
	}
	fmt.Fprintf(stdout, "Removed %s account #%d\n", profile.Agent, profile.Number)
	return 0
}

func (s *Service) Rename(number int, name string, stdout io.Writer, stdin io.Reader, agent string, yes bool) int {
	matches := s.store.ProfilesByNumber(number, agent)
	if len(matches) == 0 {
		fmt.Fprintf(stdout, "No saved account matches #%d.\n", number)
		return 1
	}
	profile, ok := s.resolveProfile(matches, stdout, stdin, "Which account should be renamed? ", yes)
	if !ok {
		return 1
	}
	if _, err := s.store.RenameProfile(profile, name); err != nil {
		fmt.Fprintln(stdout, err)
		return 1
	}
	fmt.Fprintf(stdout, "Renamed %s account #%d to %s\n", profile.Agent, profile.Number, name)
	return 0
}

type detectedAccount struct {
	provider provider.Provider
	account  model.ActiveAccount
}

func (s *Service) SaveCandidates(agent string) []model.SaveCandidate {
	s.syncActiveProfiles(agent)
	detected := s.detectedAccounts(agent)
	usageMetadata := map[model.MetadataKey]model.Metadata{}
	if s.usageProvider != nil {
		usageMetadata = s.usageProvider.Load(s.home)
	}
	var candidates []model.SaveCandidate
	for _, item := range detected {
		metadata := usage.MergeAccountWithUsageMetadata(item.account, usageMetadata)
		candidate := model.SaveCandidate{
			Agent:       item.account.Agent,
			DisplayName: item.account.DisplayName,
			Label:       item.account.Label,
			Fingerprint: item.account.Fingerprint,
			Source:      item.account.Source,
			AuthFiles:   append([]string{}, item.account.AuthFiles...),
			Metadata:    metadata,
		}
		if duplicate, ok := s.store.FindDuplicate(item.account); ok {
			candidate.DuplicateNumber = duplicate.Number
		} else {
			candidate.SaveNumber = s.store.NextNumber(item.account.Agent)
		}
		candidates = append(candidates, candidate)
	}
	return candidates
}

func (s *Service) detectedAccounts(agent string) []detectedAccount {
	var detected []detectedAccount
	for _, provider := range s.providers {
		if agent != "" && provider.Agent() != agent {
			continue
		}
		if account, ok := provider.Detect(s.home); ok {
			detected = append(detected, detectedAccount{provider: provider, account: account})
		}
	}
	return detected
}

const statusLineInputLimit = 1 << 20

func (s *Service) RecordUsage(stdin io.Reader, stderr io.Writer) int {
	input, err := io.ReadAll(io.LimitReader(stdin, statusLineInputLimit))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	line, ok := usage.ParseStatusLine(input)
	if !ok || os.Getenv("ANTHROPIC_API_KEY") != "" || os.Getenv("ANTHROPIC_AUTH_TOKEN") != "" || os.Getenv("CLAUDE_CONFIG_DIR") != "" {
		return 0
	}
	now := time.Now().Unix()
	tokenID, err := usage.ObserveStatusLine(s.home, line, s.activeClaudeTokenID, now)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if tokenID == "" {
		return 0
	}
	for _, profile := range s.store.ListProfiles("claude") {
		if profile.Metadata.String(model.MetadataTokenID) != tokenID {
			continue
		}
		if err := usage.SaveClaudeUsage(s.store.ProfileDir("claude", profile.Number), line.Usage, now); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	return 0
}

func (s *Service) activeClaudeTokenID() string {
	if token := strings.TrimSpace(os.Getenv("CLAUDE_CODE_OAUTH_TOKEN")); token != "" {
		return identity.ClaudeAccessTokenID(token)
	}
	for _, item := range s.detectedAccounts("claude") {
		return item.account.Metadata.String(model.MetadataTokenID)
	}
	return ""
}

func (s *Service) syncActiveProfiles(agent string) {
	for _, item := range s.detectedAccounts(agent) {
		duplicate, ok := s.store.FindDuplicate(item.account)
		if !ok || !s.needsUpdate(item.account, duplicate) || s.verifyLogin(item) != nil {
			continue
		}
		_, _, _ = s.updateAccount(item.provider, item.account, duplicate)
	}
}

func (s *Service) verifyLogin(item detectedAccount) error {
	claude, ok := item.provider.(*provider.ClaudeProvider)
	if !ok || s.claudeIdentity == nil || item.account.Metadata.String(model.MetadataCredentialKind) == identity.CredentialKindToken {
		return nil
	}
	return claude.VerifyLogin(s.home, provider.ActiveAccountIdentity{
		Fingerprint:      item.account.Fingerprint,
		AccountUUID:      item.account.Metadata.String(model.MetadataAccountUUID),
		OrganizationUUID: item.account.Metadata.String(model.MetadataOrganizationUUID),
	}, s.claudeIdentity, time.Now().UnixMilli())
}

func (s *Service) needsUpdate(account model.ActiveAccount, duplicate model.Profile) bool {
	if duplicate.Fingerprint == account.Fingerprint {
		return false
	}
	_, saved := s.store.CredentialsHolder(account)
	return !saved
}

func (s *Service) saveAccount(provider provider.Provider, account model.ActiveAccount) (model.Profile, error) {
	profile, err := s.store.CreateProfile(account)
	if err != nil {
		return model.Profile{}, err
	}
	if err := provider.SaveSnapshot(s.home, account, s.store.ProfileDir(profile.Agent, profile.Number)); err != nil {
		s.store.RemoveProfile(profile.Agent, profile.Number)
		return model.Profile{}, err
	}
	return profile, nil
}

func (s *Service) updateAccount(provider provider.Provider, account model.ActiveAccount, existing model.Profile) (model.Profile, []model.Profile, error) {
	if existing.Metadata.String(model.MetadataCredentialKind) == identity.CredentialKindToken {
		account.Label = existing.Label
	}
	if err := provider.SaveSnapshot(s.home, account, s.store.ProfileDir(existing.Agent, existing.Number)); err != nil {
		return model.Profile{}, nil, err
	}
	profile, err := s.store.UpdateProfile(existing, account)
	if err != nil {
		return model.Profile{}, nil, err
	}
	removed := s.store.RemoveDuplicateProfiles(account, profile.Number)
	return profile, removed, nil
}

func (s *Service) resolveProfile(matches []model.Profile, stdout io.Writer, stdin io.Reader, prompt string, yes bool) (model.Profile, bool) {
	if len(matches) == 1 {
		return matches[0], true
	}
	if yes {
		fmt.Fprintln(stdout, "Multiple accounts match. Pass --agent to avoid prompting.")
		return model.Profile{}, false
	}
	fmt.Fprintf(stdout, "Multiple accounts match #%d\n\n", matches[0].Number)
	for index, profile := range matches {
		fmt.Fprintf(stdout, "  %d  %-6s  %s\n", index+1, profile.Agent, profile.Label)
	}
	fmt.Fprintln(stdout)
	selected := selectOne(stdout, bufio.NewReader(stdin), prompt, len(matches))
	if selected < 0 {
		fmt.Fprintln(stdout, "Canceled.")
		return model.Profile{}, false
	}
	return matches[selected], true
}

func (s *Service) activeProfileKeys() map[string]bool {
	active := map[string]bool{}
	for _, item := range s.detectedAccounts("") {
		profile, ok := s.store.FindDuplicate(item.account)
		if !ok {
			continue
		}
		if holder, saved := s.store.CredentialsHolder(item.account); saved && profile.Fingerprint != item.account.Fingerprint {
			profile = holder
		}
		active[profileKey(profile)] = true
	}
	return active
}

func (s *Service) markActiveProfiles(profiles []model.Profile) {
	active := s.activeProfileKeys()
	for index := range profiles {
		profiles[index].Active = active[profileKey(profiles[index])]
	}
}

func profileKey(profile model.Profile) string {
	return profile.Agent + ":" + strconv.Itoa(profile.Number)
}

func (s *Service) withUsageMetadata(profiles []model.Profile) []model.Profile {
	if len(profiles) == 0 || s.usageProvider == nil {
		return profiles
	}
	return usage.MergeProfilesWithUsageMetadata(profiles, s.usageProvider.Load(s.home))
}

type profileGroup struct {
	name     string
	profiles []model.Profile
}

func groupProfiles(profiles []model.Profile) []profileGroup {
	grouped := map[string][]model.Profile{}
	order := []string{}
	for _, profile := range profiles {
		name := profile.DisplayName
		if _, ok := grouped[name]; !ok {
			order = append(order, name)
		}
		grouped[name] = append(grouped[name], profile)
	}
	var groups []profileGroup
	for _, name := range order {
		groups = append(groups, profileGroup{name: name, profiles: grouped[name]})
	}
	return groups
}

func parseNumber(args []string, stderr io.Writer) (int, []string, bool) {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "missing account number")
		return 0, nil, false
	}
	number, err := strconv.Atoi(args[0])
	if err != nil {
		fmt.Fprintf(stderr, "invalid account number: %s\n", args[0])
		return 0, nil, false
	}
	return number, args[1:], true
}

func hasArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func parseTokenOptions(args []string, stderr io.Writer) (string, bool) {
	label := ""
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--token":
		case "--label":
			if index+1 >= len(args) {
				fmt.Fprintln(stderr, "--label requires a value")
				return "", false
			}
			index++
			label = strings.TrimSpace(args[index])
		case "--agent":
			if index+1 >= len(args) || args[index+1] != "claude" {
				fmt.Fprintln(stderr, "--token saves Claude accounts only")
				return "", false
			}
			index++
		default:
			fmt.Fprintf(stderr, "unknown option: %s\n", args[index])
			return "", false
		}
	}
	if label == "" || strings.EqualFold(label, "unknown") {
		fmt.Fprintln(stderr, "--token requires --label NAME")
		return "", false
	}
	return label, true
}

func parseOptions(args []string, allowJSON bool, stdout, stderr io.Writer) (options, bool) {
	opts := options{}
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch arg {
		case "--help", "-h":
			printHelp(stdout)
			return opts, false
		case "--agent":
			if index+1 >= len(args) {
				fmt.Fprintln(stderr, "--agent requires a value")
				return opts, false
			}
			index++
			if args[index] != "claude" && args[index] != "codex" {
				fmt.Fprintf(stderr, "unsupported agent: %s\n", args[index])
				return opts, false
			}
			opts.agent = args[index]
		case "--yes", "-y":
			opts.yes = true
		case "--json":
			if !allowJSON {
				fmt.Fprintln(stderr, "--json is not supported for this command")
				return opts, false
			}
			opts.json = true
		default:
			fmt.Fprintf(stderr, "unknown option: %s\n", arg)
			return opts, false
		}
	}
	return opts, true
}

func confirm(stdout io.Writer, reader *bufio.Reader, prompt string) bool {
	fmt.Fprint(stdout, prompt)
	flush(stdout)
	answer, _ := reader.ReadString('\n')
	fmt.Fprintln(stdout)
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes"
}

func confirmDefaultYes(stdout io.Writer, reader *bufio.Reader, prompt string) bool {
	fmt.Fprint(stdout, prompt)
	flush(stdout)
	answer, _ := reader.ReadString('\n')
	fmt.Fprintln(stdout)
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "" || answer == "y" || answer == "yes"
}

func selectOne(stdout io.Writer, reader *bufio.Reader, prompt string, count int) int {
	choices := make([]string, count)
	for index := range choices {
		choices[index] = strconv.Itoa(index + 1)
	}
	fmt.Fprintf(stdout, "%s[%s/N, Enter cancels] ", prompt, strings.Join(choices, "/"))
	flush(stdout)
	answer, _ := reader.ReadString('\n')
	fmt.Fprintln(stdout)
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer == "" || answer == "n" || answer == "no" {
		return -1
	}
	selected, err := strconv.Atoi(answer)
	if err != nil || selected < 1 || selected > count {
		return -1
	}
	return selected - 1
}

func selectOrAll(stdout io.Writer, reader *bufio.Reader, prompt string, count int) []int {
	choices := make([]string, count)
	for index := range choices {
		choices[index] = strconv.Itoa(index + 1)
	}
	fmt.Fprintf(stdout, "%s[%s Enter save all] ", prompt, strings.Join(choices, "/"))
	flush(stdout)
	answer, _ := reader.ReadString('\n')
	fmt.Fprintln(stdout)
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer == "" || answer == "a" || answer == "all" {
		selected := make([]int, count)
		for index := range selected {
			selected[index] = index
		}
		return selected
	}
	if answer == "n" || answer == "no" {
		return nil
	}
	selected, err := strconv.Atoi(answer)
	if err != nil || selected < 1 || selected > count {
		return nil
	}
	return []int{selected - 1}
}

func writeJSON(stdout io.Writer, value any) {
	data, _ := json.MarshalIndent(value, "", "  ")
	stdout.Write(data)
	fmt.Fprintln(stdout)
}

func printHelp(stdout io.Writer) {
	fmt.Fprintln(stdout, "usage: ags [-h] {save,list,remove,update} ...")
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Switch accounts across AI coding agents without logging out.")
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "positional arguments:")
	fmt.Fprintln(stdout, "  {save,list,remove,update}")
	fmt.Fprintln(stdout, "    save                save the current active account")
	fmt.Fprintln(stdout, "    save --token --label NAME")
	fmt.Fprintln(stdout, "                        save a Claude token from `claude setup-token` read on stdin")
	fmt.Fprintln(stdout, "    list                show saved accounts and active status")
	fmt.Fprintln(stdout, "    remove              remove a saved account number")
	fmt.Fprintln(stdout, "    rename N NAME       show a saved account as NAME")
	fmt.Fprintln(stdout, "    update              update ags to the latest release")
	fmt.Fprintln(stdout, "    record-usage        record a Claude token's usage from status line input on stdin")
}

type flusher interface {
	Flush()
}

func flush(writer io.Writer) {
	if flusher, ok := writer.(flusher); ok {
		flusher.Flush()
	}
}

func sortedKeys[T any](input map[string]T) []string {
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
