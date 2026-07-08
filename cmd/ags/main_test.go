package main

import "testing"

func TestMainPackageBuilds(t *testing.T) {
	if version == "" {
		t.Fatal("version should be set")
	}
}
