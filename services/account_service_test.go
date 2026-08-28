package services

import (
	"testing"

	"github.com/wfu-work/proxy-api-lib/chatgpt"
)

func TestNormalizeUsageRateLimitSupportsProSingleWeeklyWindow(t *testing.T) {
	raw := []byte(`{
		"planType":"pro",
		"rateLimit":{
			"allowed":true,
			"limitReached":false,
			"primaryWindow":{"usedPercent":"42.5","resetAt":"2026-09-04T13:33:45Z"}
		}
	}`)

	limit := normalizeUsageRateLimit(raw, nil, true)
	if limit == nil || limit.PrimaryWindow == nil {
		t.Fatal("expected a normalized primary window")
	}
	if limit.PrimaryWindow.UsedPercent == nil || *limit.PrimaryWindow.UsedPercent != 42.5 {
		t.Fatalf("unexpected used percent: %#v", limit.PrimaryWindow.UsedPercent)
	}
	if limit.PrimaryWindow.LimitWindowSeconds == nil || *limit.PrimaryWindow.LimitWindowSeconds != usageWeeklyWindowSeconds {
		t.Fatalf("Pro single window should be normalized to 7 days, got %#v", limit.PrimaryWindow.LimitWindowSeconds)
	}
	if limit.PrimaryWindow.ResetAt == nil || *limit.PrimaryWindow.ResetAt <= 0 {
		t.Fatal("expected an RFC3339 reset timestamp")
	}
	if limit.Allowed == nil || !*limit.Allowed || limit.LimitReached == nil || *limit.LimitReached {
		t.Fatalf("unexpected group flags: allowed=%v limitReached=%v", limit.Allowed, limit.LimitReached)
	}
}

func TestNormalizeUsageRateLimitKeepsPlusWindowsIndependent(t *testing.T) {
	primaryUsed := 56.0
	secondaryUsed := 25.0
	parsed := &chatgpt.RateLimit{
		PrimaryWindow:   &chatgpt.RateLimitWindow{UsedPercent: &primaryUsed},
		SecondaryWindow: &chatgpt.RateLimitWindow{UsedPercent: &secondaryUsed},
	}
	raw := []byte(`{
		"rate_limit":{
			"primary_window":{"used_percent":56,"limit_window_seconds":18000},
			"secondary_window":{"used_percent":25,"limit_window_seconds":604800}
		}
	}`)

	limit := normalizeUsageRateLimit(raw, parsed, false)
	if limit == nil || limit.PrimaryWindow == nil || limit.SecondaryWindow == nil {
		t.Fatal("expected both Plus windows")
	}
	if got := *limit.PrimaryWindow.UsedPercent; got != 56 {
		t.Fatalf("unexpected 5-hour usage: %v", got)
	}
	if got := *limit.SecondaryWindow.UsedPercent; got != 25 {
		t.Fatalf("unexpected 7-day usage: %v", got)
	}
	if got := *limit.PrimaryWindow.LimitWindowSeconds; got != usageFiveHourWindowSeconds {
		t.Fatalf("unexpected 5-hour duration: %v", got)
	}
	if got := *limit.SecondaryWindow.LimitWindowSeconds; got != usageWeeklyWindowSeconds {
		t.Fatalf("unexpected 7-day duration: %v", got)
	}
}

func TestNormalizeUsageRateLimitDerivesUsedPercentFromRemaining(t *testing.T) {
	raw := []byte(`{"rate_limit":{"primary_window":{"remaining_percent":12.5,"limit_window_seconds":604800}}}`)
	limit := normalizeUsageRateLimit(raw, nil, false)
	if limit == nil || limit.PrimaryWindow == nil || limit.PrimaryWindow.UsedPercent == nil {
		t.Fatal("expected used percent derived from remaining percent")
	}
	if got := *limit.PrimaryWindow.UsedPercent; got != 87.5 {
		t.Fatalf("unexpected derived used percent: %v", got)
	}
}
