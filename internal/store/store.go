package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/annapo99/agent-switch/internal/model"
)

type Store struct {
	home string
}

func New(home string) *Store {
	return &Store{home: home}
}

func (s *Store) ProfileDir(agent string, number int) string {
	return filepath.Join(s.home, ".agent-switch", "profiles", agent, strconv.Itoa(number))
}

func (s *Store) NextNumber(agent string) int {
	maxNumber := 0
	for _, profile := range s.ListProfiles(agent) {
		if profile.Number > maxNumber {
			maxNumber = profile.Number
		}
	}
	return maxNumber + 1
}

func (s *Store) CreateProfile(account model.ActiveAccount) (model.Profile, error) {
	number := s.NextNumber(account.Agent)
	profile := profileFromAccount(account, number, time.Now().UTC().Format(time.RFC3339Nano))
	dir := s.ProfileDir(account.Agent, number)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return model.Profile{}, err
	}
	if err := writeProfile(filepath.Join(dir, "manifest.json"), profile); err != nil {
		return model.Profile{}, err
	}
	return profile, nil
}

func (s *Store) UpdateProfile(existing model.Profile, account model.ActiveAccount) (model.Profile, error) {
	createdAt := existing.CreatedAt
	if createdAt == "" {
		createdAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	profile := profileFromAccount(account, existing.Number, createdAt)
	dir := s.ProfileDir(existing.Agent, existing.Number)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return model.Profile{}, err
	}
	if err := writeProfile(filepath.Join(dir, "manifest.json"), profile); err != nil {
		return model.Profile{}, err
	}
	return profile, nil
}

func (s *Store) ListProfiles(agent string) []model.Profile {
	root := filepath.Join(s.home, ".agent-switch", "profiles")
	agents := []string{}
	if agent != "" {
		agents = append(agents, agent)
	} else {
		entries, err := os.ReadDir(root)
		if err != nil {
			return nil
		}
		for _, entry := range entries {
			if entry.IsDir() {
				agents = append(agents, entry.Name())
			}
		}
		sort.Strings(agents)
	}

	var profiles []model.Profile
	for _, agentName := range agents {
		agentDir := filepath.Join(root, agentName)
		entries, err := os.ReadDir(agentDir)
		if err != nil {
			continue
		}
		var numbers []int
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			number, err := strconv.Atoi(entry.Name())
			if err == nil {
				numbers = append(numbers, number)
			}
		}
		sort.Ints(numbers)
		for _, number := range numbers {
			profile, err := readProfile(filepath.Join(agentDir, strconv.Itoa(number), "manifest.json"))
			if err == nil {
				profiles = append(profiles, profile)
			}
		}
	}
	return profiles
}

func (s *Store) FindDuplicate(account model.ActiveAccount) (model.Profile, bool) {
	for _, profile := range s.ListProfiles(account.Agent) {
		if profileMatchesAccount(profile, account) {
			return profile, true
		}
	}
	return model.Profile{}, false
}

func (s *Store) RemoveDuplicateProfiles(account model.ActiveAccount, keepNumber int) []model.Profile {
	removed := []model.Profile{}
	for _, profile := range s.ListProfiles(account.Agent) {
		if profile.Number == keepNumber || !profileMatchesAccount(profile, account) {
			continue
		}
		if s.RemoveProfile(profile.Agent, profile.Number) {
			removed = append(removed, profile)
		}
	}
	return removed
}

func (s *Store) ProfilesByNumber(number int, agent string) []model.Profile {
	var matches []model.Profile
	for _, profile := range s.ListProfiles(agent) {
		if profile.Number == number {
			matches = append(matches, profile)
		}
	}
	return matches
}

func (s *Store) RemoveProfile(agent string, number int) bool {
	dir := s.ProfileDir(agent, number)
	if !dirExists(dir) {
		return false
	}
	err := os.RemoveAll(dir)
	return err == nil && !dirExists(dir)
}

func readProfile(path string) (model.Profile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return model.Profile{}, err
	}
	var profile model.Profile
	if err := json.Unmarshal(data, &profile); err != nil {
		return model.Profile{}, err
	}
	if profile.Metadata == nil {
		profile.Metadata = model.Metadata{}
	}
	return profile, nil
}

func dirExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil || !errors.Is(err, os.ErrNotExist)
}

func cloneMetadata(metadata model.Metadata) model.Metadata {
	if metadata == nil {
		return model.Metadata{}
	}
	cloned := model.Metadata{}
	for key, value := range metadata {
		cloned[key] = value
	}
	return cloned
}

func profileFromAccount(account model.ActiveAccount, number int, createdAt string) model.Profile {
	return model.Profile{
		Agent:       account.Agent,
		DisplayName: account.DisplayName,
		Number:      number,
		Label:       account.Label,
		Fingerprint: account.Fingerprint,
		Source:      account.Source,
		AuthFiles:   append([]string{}, account.AuthFiles...),
		CreatedAt:   createdAt,
		Metadata:    cloneMetadata(account.Metadata),
	}
}

func writeProfile(path string, profile model.Profile) error {
	data, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

func profileMatchesAccount(profile model.Profile, account model.ActiveAccount) bool {
	if profile.Agent != account.Agent {
		return false
	}
	if profile.Fingerprint != "" && account.Fingerprint != "" && profile.Fingerprint == account.Fingerprint {
		return true
	}
	profileLabel := normalizedIdentityLabel(profile.Label)
	accountLabel := normalizedIdentityLabel(account.Label)
	return profileLabel != "" && profileLabel == accountLabel
}

func normalizedIdentityLabel(label string) string {
	normalized := strings.ToLower(strings.TrimSpace(label))
	if normalized == "" || normalized == "unknown" {
		return ""
	}
	return normalized
}
