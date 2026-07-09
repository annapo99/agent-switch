package app

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

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
	return &Service{
		home:          home,
		version:       version,
		providers:     provider.DefaultProviders(),
		usageProvider: usageProvider,
		store:         store.New(home),
	}
}

func (s *Service) Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		printHelp(stdout)
		return 0
	}
	command := args[0]
	switch command {
	case "save":
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
		return s.List(stdout, opts.agent, opts.json)
	case "current":
		opts, ok := parseOptions(args[1:], true, stdout, stderr)
		if !ok {
			return 2
		}
		return s.CurrentJSONAware(stdout, opts.agent, opts.json)
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
	case "update":
		return runUpdater(s.home, s.version, stdout, stderr)
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
			if item.duplicate.Fingerprint != item.account.Fingerprint {
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
	provider := provider.ProviderByAgent(profile.Agent)
	if provider == nil {
		fmt.Fprintf(stdout, "Unsupported agent: %s\n", profile.Agent)
		return 1
	}
	if err := provider.ApplySnapshot(s.home, s.store.ProfileDir(profile.Agent, profile.Number)); err != nil {
		fmt.Fprintln(stdout, err)
		return 1
	}
	name := profile.DisplayName
	if name == "" {
		name = profile.Agent
	}
	if profile.Agent == "claude" {
		fmt.Fprintf(stdout, "Switched %s to account #%d. Running sessions may take up to ~30s to pick it up.\n", name, profile.Number)
		return 0
	}
	fmt.Fprintf(stdout, "Switched %s to account #%d\n", name, profile.Number)
	return 0
}

func (s *Service) List(stdout io.Writer, agent string, jsonOutput bool) int {
	profiles := s.withUsageMetadata(s.store.ListProfiles(agent))
	s.markActiveProfiles(profiles)
	if jsonOutput {
		writeJSON(stdout, profiles)
		return 0
	}
	if len(profiles) == 0 {
		fmt.Fprintln(stdout, "No saved accounts.")
		return 0
	}
	color := shouldColor(stdout)
	fmt.Fprintln(stdout, "Saved accounts")
	fmt.Fprintln(stdout)
	for _, group := range groupProfiles(profiles) {
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

func (s *Service) Current(stdout io.Writer, agent string) int {
	return s.CurrentJSONAware(stdout, agent, false)
}

func (s *Service) CurrentJSONAware(stdout io.Writer, agent string, jsonOutput bool) int {
	detected := s.detectedAccounts(agent)
	var current []model.Profile
	for _, item := range detected {
		if duplicate, ok := s.store.FindDuplicate(item.account); ok {
			duplicate.Active = true
			current = append(current, duplicate)
		}
	}
	current = s.withUsageMetadata(current)
	if jsonOutput {
		writeJSON(stdout, current)
		return 0
	}
	if len(current) == 0 {
		fmt.Fprintln(stdout, "No active saved accounts.")
		return 0
	}
	color := shouldColor(stdout)
	fmt.Fprintln(stdout, "Current accounts")
	fmt.Fprintln(stdout)
	for _, group := range groupProfiles(current) {
		fmt.Fprintln(stdout, render.AgentHeading(group.name, color))
		for _, profile := range group.profiles {
			profile.Active = true
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

type detectedAccount struct {
	provider provider.Provider
	account  model.ActiveAccount
}

func (s *Service) SaveCandidates(agent string) []model.SaveCandidate {
	detected := s.detectedAccounts(agent)
	usageMetadata := map[model.MetadataKey]model.Metadata{}
	if s.usageProvider != nil {
		usageMetadata = s.usageProvider.Load(s.home)
	}
	var candidates []model.SaveCandidate
	for _, item := range detected {
		key := model.MetadataKey{Agent: item.account.Agent, Label: strings.ToLower(item.account.Label)}
		metadata := mergedMetadata(item.account.Metadata, usageMetadata[key])
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

func (s *Service) saveAccount(provider provider.Provider, account model.ActiveAccount) (model.Profile, error) {
	profile, err := s.store.CreateProfile(account)
	if err != nil {
		return model.Profile{}, err
	}
	if err := provider.SaveSnapshot(s.home, account, s.store.ProfileDir(profile.Agent, profile.Number)); err != nil {
		return model.Profile{}, err
	}
	return profile, nil
}

func (s *Service) updateAccount(provider provider.Provider, account model.ActiveAccount, existing model.Profile) (model.Profile, []model.Profile, error) {
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

func (s *Service) activeFingerprints() map[string]string {
	active := map[string]string{}
	for _, item := range s.detectedAccounts("") {
		active[item.account.Agent] = item.account.Fingerprint
	}
	return active
}

func (s *Service) markActiveProfiles(profiles []model.Profile) {
	active := s.activeFingerprints()
	for index := range profiles {
		profiles[index].Active = active[profiles[index].Agent] == profiles[index].Fingerprint
	}
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
	fmt.Fprintln(stdout, "usage: ags [-h] {save,use,list,current,remove,update} ...")
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Switch accounts across AI coding agents without logging out.")
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "positional arguments:")
	fmt.Fprintln(stdout, "  {save,use,list,current,remove,update}")
	fmt.Fprintln(stdout, "    save                save the current active account")
	fmt.Fprintln(stdout, "    use                 switch to a saved account number")
	fmt.Fprintln(stdout, "    list                list saved accounts")
	fmt.Fprintln(stdout, "    current             show active saved accounts")
	fmt.Fprintln(stdout, "    remove              remove a saved account number")
	fmt.Fprintln(stdout, "    update              update ags to the latest release")
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

func mergedMetadata(values ...model.Metadata) model.Metadata {
	merged := model.Metadata{}
	for _, metadata := range values {
		for key, value := range metadata {
			merged[key] = value
		}
	}
	return merged
}
