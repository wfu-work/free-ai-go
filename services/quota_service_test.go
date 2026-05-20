package services

import "testing"

func TestNormalizeQuotaInputDefaultsWindow(t *testing.T) {
	input := normalizeQuotaInput(QuotaInput{
		AccountGuid: "acc",
		WindowType:  "5h",
		TotalTokens: 100,
	})
	if input.RemainingTokens != 100 {
		t.Fatalf("remaining = %d", input.RemainingTokens)
	}
	if input.ResetAt == 0 {
		t.Fatal("expected reset time")
	}
	if input.NextRefreshAt == 0 {
		t.Fatal("expected next refresh time")
	}
}

func TestNormalizeQuotaInputMarksExhaustedByPercent(t *testing.T) {
	input := normalizeQuotaInput(QuotaInput{
		AccountGuid:     "acc",
		WindowType:      "daily",
		TotalAmount:     90,
		RemainingAmount: 0.11,
		UsedAmount:      89.89,
	})
	if input.Status != "exhausted" {
		t.Fatalf("expected exhausted status, got %q", input.Status)
	}
}
