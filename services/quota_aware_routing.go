package services

import (
	"errors"
	"math"
	"strings"
	"time"

	"github.com/wfu-work/free-ai-go/domains"
	"github.com/wfu-work/nav-common-go-lib/global"
)

const (
	routeQuotaMaxAge        = 15 * time.Minute
	routeQuotaMinFactor     = 0.35
	routeQuotaMaxFactor     = 1.70
	routeQuotaMaxResetBoost = 0.20
)

type accountQuotaRouteSnapshot struct {
	Known         bool
	Remaining     float64
	ResetAt       int64
	WindowSeconds int64
}

// loadQuotaAdaptiveRouteFactors reads all candidate quota windows in one query.
// Callers deliberately ignore the returned error and fall back to ordinary
// adaptive routing when the database or quota snapshot is unavailable.
func loadQuotaAdaptiveRouteFactors(accounts []domains.Account, now time.Time) (map[string]float64, error) {
	factors := make(map[string]float64, len(accounts))
	guids := make([]string, 0, len(accounts))
	for _, account := range accounts {
		guid := strings.TrimSpace(account.Guid)
		if guid == "" {
			continue
		}
		guids = append(guids, guid)
		factors[guid] = 1
	}
	if len(guids) == 0 {
		return factors, nil
	}
	if global.NAV_DB == nil {
		return factors, errors.New("quota routing database is unavailable")
	}

	var quotas []domains.AccountQuota
	if err := global.NAV_DB.Where("account_guid IN ?", guids).Find(&quotas).Error; err != nil {
		return factors, err
	}
	return quotaAdaptiveRouteFactors(guids, quotas, now), nil
}

func quotaAdaptiveRouteFactors(accountGuids []string, quotas []domains.AccountQuota, now time.Time) map[string]float64 {
	snapshots := quotaRouteSnapshots(quotas, now)
	factors := make(map[string]float64, len(accountGuids))
	for _, guid := range accountGuids {
		factor := 1.0
		if snapshot := snapshots[guid]; snapshot.Known {
			factor = quotaRouteFactor(snapshot, now)
		}
		factors[guid] = factor
	}
	return factors
}

func quotaRouteSnapshots(quotas []domains.AccountQuota, now time.Time) map[string]accountQuotaRouteSnapshot {
	snapshots := make(map[string]accountQuotaRouteSnapshot)
	nowMs := now.UnixMilli()
	oldestFreshMs := now.Add(-routeQuotaMaxAge).UnixMilli()
	for _, quota := range quotas {
		if strings.TrimSpace(quota.AccountGuid) == "" || quota.LastSyncedAt <= 0 || quota.LastSyncedAt < oldestFreshMs {
			continue
		}
		if quota.ResetAt > 0 && quota.ResetAt <= nowMs {
			continue
		}

		exhausted := quota.Status == domains.QuotaStatusExhausted ||
			quota.LimitReached != nil && *quota.LimitReached ||
			quota.Allowed != nil && !*quota.Allowed
		if quota.UsedPercent == nil && !exhausted {
			continue
		}
		remaining := 0.0
		if !exhausted {
			remaining = 1 - clampFloat(*quota.UsedPercent/100, 0, 1)
		}
		candidate := accountQuotaRouteSnapshot{
			Known:         true,
			Remaining:     remaining,
			ResetAt:       quota.ResetAt,
			WindowSeconds: quota.LimitWindowSeconds,
		}
		current := snapshots[quota.AccountGuid]
		if !current.Known || candidate.Remaining < current.Remaining ||
			candidate.Remaining == current.Remaining && earlierPositiveReset(candidate.ResetAt, current.ResetAt) {
			snapshots[quota.AccountGuid] = candidate
		}
	}
	return snapshots
}

func quotaRouteFactor(snapshot accountQuotaRouteSnapshot, now time.Time) float64 {
	if !snapshot.Known {
		return 1
	}
	remaining := clampFloat(snapshot.Remaining, 0, 1)
	factor := 0.4 + 1.2*remaining
	if snapshot.ResetAt > now.UnixMilli() && snapshot.WindowSeconds > 0 {
		untilReset := time.Duration(snapshot.ResetAt-now.UnixMilli()) * time.Millisecond
		window := time.Duration(snapshot.WindowSeconds) * time.Second
		progress := 1 - clampFloat(float64(untilReset)/float64(window), 0, 1)
		factor *= 1 + routeQuotaMaxResetBoost*progress
	}
	return clampFloat(factor, routeQuotaMinFactor, routeQuotaMaxFactor)
}

func earlierPositiveReset(left, right int64) bool {
	if left <= 0 {
		return false
	}
	return right <= 0 || left < right
}

func clampFloat(value, minimum, maximum float64) float64 {
	return math.Max(minimum, math.Min(maximum, value))
}
