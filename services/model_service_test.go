package services

import "testing"

func TestNormalizeModelAccountGroupKeepsGlobalEmpty(t *testing.T) {
	if got := normalizeModelAccountGroup(" "); got != "" {
		t.Fatalf("expected empty global account group, got %q", got)
	}
	if got := normalizeModelAccountGroup(" default "); got != "default" {
		t.Fatalf("expected trimmed account group, got %q", got)
	}
}
