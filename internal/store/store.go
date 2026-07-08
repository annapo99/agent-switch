package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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
	profile := model.Profile{
		Agent:       account.Agent,
		DisplayName: account.DisplayName,
		Number:      number,
		Label:       account.Label,
		Fingerprint: account.Fingerprint,
		Source:      account.Source,
		AuthFiles:   append([]string{}, account.AuthFiles...),
		CreatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		Metadata:    cloneMetadata(account.Metadata),
	}
	dir := s.ProfileDir(account.Agent, number)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return model.Profile{}, err
	}
	data, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return model.Profile{}, err
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0o600); err != nil {
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
		if profile.Fingerprint == account.Fingerprint {
			return profile, true
		}
	}
	return model.Profile{}, false
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
