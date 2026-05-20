package services

import "testing"

func TestProviderDefaultAPIBaseURLFreeModel(t *testing.T) {
	if got := providerDefaultAPIBaseURL("freemodel"); got != freeModelAPIBaseURL {
		t.Fatalf("expected FreeModel base URL %q, got %q", freeModelAPIBaseURL, got)
	}
}

func TestProviderDefaultAPIBaseURLAiok(t *testing.T) {
	if got := providerDefaultAPIBaseURL("aiok"); got != aiokAPIBaseURL {
		t.Fatalf("expected Aiok base URL %q, got %q", aiokAPIBaseURL, got)
	}
}

func TestProviderDefaultAPIBaseURLTokeni(t *testing.T) {
	if got := providerDefaultAPIBaseURL("tokeni"); got != tokeniAPIBaseURL {
		t.Fatalf("expected Tokeni base URL %q, got %q", tokeniAPIBaseURL, got)
	}
}

func TestNormalizeUsageQueryType(t *testing.T) {
	if got := normalizeUsageQueryType(" Tokeni "); got != "tokeni" {
		t.Fatalf("expected normalized usage query type tokeni, got %q", got)
	}
}

func TestParseTokeniUsageResponseBalance(t *testing.T) {
	stats, err := parseTokeniUsageResponse([]byte(`{"balance":12.34}`))
	if err != nil {
		t.Fatalf("parse tokeni usage response failed: %v", err)
	}
	if stats.Balance != 12.34 {
		t.Fatalf("expected balance 12.34, got %v", stats.Balance)
	}
}

func TestParseTokeniUsageResponseNestedBalance(t *testing.T) {
	stats, err := parseTokeniUsageResponse([]byte(`{"data":{"balance":"5.67"}}`))
	if err != nil {
		t.Fatalf("parse tokeni usage response failed: %v", err)
	}
	if stats.Balance != 5.67 {
		t.Fatalf("expected balance 5.67, got %v", stats.Balance)
	}
}
