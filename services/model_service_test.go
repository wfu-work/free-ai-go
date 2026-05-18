package services

import "testing"

func TestModelAliasMatchesJSONAndCSV(t *testing.T) {
	if !modelAliasMatches(`["gpt-4o","fast-model"]`, "fast-model") {
		t.Fatal("expected JSON alias to match")
	}
	if !modelAliasMatches("gpt-4.1, gpt-main", "gpt-main") {
		t.Fatal("expected CSV alias to match")
	}
	if modelAliasMatches("gpt-4.1", "gpt-4o") {
		t.Fatal("unexpected alias match")
	}
}
