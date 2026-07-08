package model

import "testing"

func TestMetadataStringReadsOnlyStrings(t *testing.T) {
	metadata := Metadata{"organization_name": "Example Team", "count": 3}

	if got := metadata.String("organization_name"); got != "Example Team" {
		t.Fatalf("organization_name = %q", got)
	}
	if got := metadata.String("count"); got != "" {
		t.Fatalf("count string = %q", got)
	}
}
