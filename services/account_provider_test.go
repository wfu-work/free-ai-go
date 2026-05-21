package services

import (
	"testing"

	"github.com/wfu-work/proxy-api-lib/compat/aiok"
	proxycodexzh "github.com/wfu-work/proxy-api-lib/compat/codexzh"
	proxyfreemodel "github.com/wfu-work/proxy-api-lib/compat/freemodel"
	proxytokeni "github.com/wfu-work/proxy-api-lib/compat/tokeni"
)

func TestProviderDefaultAPIBaseURLFreeModel(t *testing.T) {
	if got := providerDefaultAPIBaseURL("freemodel"); got != proxyfreemodel.BaseURL {
		t.Fatalf("expected FreeModel base URL %q, got %q", proxyfreemodel.BaseURL, got)
	}
}

func TestProviderDefaultAPIBaseURLAiok(t *testing.T) {
	if got := providerDefaultAPIBaseURL("aiok"); got != aiok.BaseURL {
		t.Fatalf("expected Aiok base URL %q, got %q", aiok.BaseURL, got)
	}
}

func TestProviderDefaultAPIBaseURLTokeni(t *testing.T) {
	if got := providerDefaultAPIBaseURL("tokeni"); got != proxytokeni.BaseURL {
		t.Fatalf("expected Tokeni base URL %q, got %q", proxytokeni.BaseURL, got)
	}
}

func TestNormalizeUsageQueryType(t *testing.T) {
	if got := normalizeUsageQueryType(" Tokeni "); got != "tokeni" {
		t.Fatalf("expected normalized usage query type tokeni, got %q", got)
	}
}

func TestParseTokeniUsageResponseBalance(t *testing.T) {
	stats, err := proxytokeni.ParseUsageResponse([]byte(`{"balance":12.34}`))
	if err != nil {
		t.Fatalf("parse tokeni usage response failed: %v", err)
	}
	if stats.Balance != 12.34 {
		t.Fatalf("expected balance 12.34, got %v", stats.Balance)
	}
}

func TestParseTokeniUsageResponseNestedBalance(t *testing.T) {
	stats, err := proxytokeni.ParseUsageResponse([]byte(`{"data":{"balance":"5.67"}}`))
	if err != nil {
		t.Fatalf("parse tokeni usage response failed: %v", err)
	}
	if stats.Balance != 5.67 {
		t.Fatalf("expected balance 5.67, got %v", stats.Balance)
	}
}

func TestCodexZHQuotaToUSDUsesProxyLibrary(t *testing.T) {
	if got := codexZHQuotaToUSD(500000); got != proxycodexzh.QuotaToUSD(500000) {
		t.Fatalf("expected codexzh quota conversion from proxy library, got %v", got)
	}
}
