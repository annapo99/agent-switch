package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/annapo99/agent-switch/internal/model"
)

func active(agent, label, fingerprint string, metadata model.Metadata) model.ActiveAccount {
	display := map[string]string{"claude": "Claude", "codex": "Codex"}[agent]
	if display == "" {
		display = agent
	}
	return model.ActiveAccount{
		Agent:       agent,
		DisplayName: display,
		Label:       label,
		Fingerprint: fingerprint,
		Source:      "fixture",
		AuthFiles:   []string{},
		Metadata:    metadata,
	}
}

func TestNextNumberIsScopedPerAgent(t *testing.T) {
	s := New(t.TempDir())
	if _, err := s.CreateProfile(active("claude", "a@example.com", "fp-a", nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProfile(active("claude", "b@example.com", "fp-b", nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProfile(active("codex", "c@example.com", "fp-c", nil)); err != nil {
		t.Fatal(err)
	}

	if got := s.NextNumber("claude"); got != 3 {
		t.Fatalf("claude next = %d", got)
	}
	if got := s.NextNumber("codex"); got != 2 {
		t.Fatalf("codex next = %d", got)
	}
	if got := s.NextNumber("gemini"); got != 1 {
		t.Fatalf("gemini next = %d", got)
	}
}

func TestCreateProfilePersistsManifestAndMetadata(t *testing.T) {
	home := t.TempDir()
	s := New(home)
	profile, err := s.CreateProfile(active("claude", "a@example.com", "fp-a", model.Metadata{
		"organization_name": "Example Team",
	}))
	if err != nil {
		t.Fatal(err)
	}

	if profile.Agent != "claude" || profile.Number != 1 {
		t.Fatalf("profile = %+v", profile)
	}
	data, err := os.ReadFile(filepath.Join(home, ".agent-switch/profiles/claude/1/manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest model.Profile
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Label != "a@example.com" || manifest.Fingerprint != "fp-a" {
		t.Fatalf("manifest = %+v", manifest)
	}
	if got := manifest.Metadata.String("organization_name"); got != "Example Team" {
		t.Fatalf("organization metadata = %q", got)
	}
}

func TestFindDuplicateUsesAgentAndStableIdentity(t *testing.T) {
	s := New(t.TempDir())
	if _, err := s.CreateProfile(active("claude", "a@example.com", "same", nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProfile(active("codex", "a@example.com", "same", nil)); err != nil {
		t.Fatal(err)
	}

	claude, ok := s.FindDuplicate(active("claude", "other@example.com", "same", nil))
	if !ok || claude.Agent != "claude" || claude.Number != 1 {
		t.Fatalf("claude duplicate = %+v ok=%v", claude, ok)
	}
	codex, ok := s.FindDuplicate(active("codex", "other@example.com", "same", nil))
	if !ok || codex.Agent != "codex" || codex.Number != 1 {
		t.Fatalf("codex duplicate = %+v ok=%v", codex, ok)
	}
	rotated, ok := s.FindDuplicate(active("claude", "A@EXAMPLE.COM", "rotated-fingerprint", nil))
	if !ok || rotated.Agent != "claude" || rotated.Number != 1 {
		t.Fatalf("rotated duplicate = %+v ok=%v", rotated, ok)
	}
}

func TestProfilesByNumberCanBeAmbiguousAcrossAgents(t *testing.T) {
	s := New(t.TempDir())
	if _, err := s.CreateProfile(active("claude", "a@example.com", "fp-a", nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProfile(active("codex", "b@example.com", "fp-b", nil)); err != nil {
		t.Fatal(err)
	}

	matches := s.ProfilesByNumber(1, "")

	if len(matches) != 2 {
		t.Fatalf("matches len = %d", len(matches))
	}
	if matches[0].Agent != "claude" || matches[1].Agent != "codex" {
		t.Fatalf("matches = %+v", matches)
	}
}

func TestRemoveProfileDeletesProfileDirectory(t *testing.T) {
	home := t.TempDir()
	s := New(home)
	if _, err := s.CreateProfile(active("claude", "a@example.com", "fp-a", nil)); err != nil {
		t.Fatal(err)
	}

	if !s.RemoveProfile("claude", 1) {
		t.Fatal("expected first remove to return true")
	}
	if _, err := os.Stat(filepath.Join(home, ".agent-switch/profiles/claude/1")); !os.IsNotExist(err) {
		t.Fatalf("profile dir still exists err=%v", err)
	}
	if s.RemoveProfile("claude", 1) {
		t.Fatal("expected second remove to return false")
	}
}
