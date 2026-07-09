package main

import "testing"

func TestMainPackageBuilds(t *testing.T) {
	if version == "" {
		t.Fatal("version should be set")
	}
}

func TestResolvedVersionUsesInjectedVersion(t *testing.T) {
	original := version
	version = "v9.9.9"
	defer func() { version = original }()

	if got := resolvedVersion(); got != "v9.9.9" {
		t.Fatalf("resolvedVersion = %q", got)
	}
}
