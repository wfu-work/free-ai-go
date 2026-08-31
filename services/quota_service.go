package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wfu-work/free-ai-go/domains"
	"github.com/wfu-work/nav-common-go-lib/global"
	commonUtils "github.com/wfu-work/nav-common-go-lib/utils"
	"github.com/wfu-work/proxy-api-lib/chatgpt"
	"gorm.io/gorm"
)

type QuotaService struct{}

var QuotaServiceApp = QuotaService{}

type QuotaInput struct {
	AccountGuid        string   `json:"accountGuid"`
	WindowType         string   `json:"windowType"`
	UsedPercent        *float64 `json:"usedPercent"`
	LimitWindowSeconds int64    `json:"limitWindowSeconds"`
	ResetAt            int64    `json:"resetAt"`
	Allowed            *bool    `json:"allowed"`
	LimitReached       *bool    `json:"limitReached"`
	Source             string   `json:"source"`
	NextRefreshAt      int64    `json:"nextRefreshAt"`
	LastSyncedAt       int64    `json:"lastSyncedAt"`
	Status             string   `json:"status"`
	Extra              string   `json:"extra"`
}

const QuotaExhaustedPercentThreshold = 99.5

// quotaRoutingSampleMaxAge limits non-authoritative quota samples used by
// routing. Official wham snapshots remain authoritative until their reset;
// response headers and active probes are only hints and must not permanently
// take an account out of the pool after a transient or partial outage.
const quotaRoutingSampleMaxAge = 15 * time.Minute

func (s QuotaService) Upsert(input QuotaInput) (domains.AccountQuota, error) {
	input.AccountGuid = strings.TrimSpace(input.AccountGuid)
	input.WindowType = strings.TrimSpace(input.WindowType)
	input.Source = strings.TrimSpace(input.Source)
	if input.AccountGuid == "" {
		return domains.AccountQuota{}, errors.New("accountGuid is required")
	}
	if input.WindowType == "" {
		return domains.AccountQuota{}, errors.New("windowType is required")
	}
	input = normalizeQuotaInput(input)
	updates := map[string]any{
		"used_percent":         input.UsedPercent,
		"limit_window_seconds": input.LimitWindowSeconds,
		"reset_at":             input.ResetAt,
		"allowed":              input.Allowed,
		"limit_reached":        input.LimitReached,
		"source":               input.Source,
		"next_refresh_at":      input.NextRefreshAt,
		"last_synced_at":       input.LastSyncedAt,
		"status":               input.Status,
		"extra":                input.Extra,
	}
	var quota domains.AccountQuota
	identity := global.NAV_DB.Where("account_guid = ? AND window_type = ?", input.AccountGuid, input.WindowType)
	// Keep authoritative /wham snapshots separate from response-header and probe
	// samples. They may describe the same window, but a normal request must not
	// overwrite the official subscription quota shown in the account card.
	if input.Source != "" {
		identity = identity.Where("source = ?", input.Source)
	}
	err := identity.First(&quota).Error
	if err == nil {
		err = global.NAV_DB.Model(&quota).Updates(updates).Error
		if err == nil {
			_ = identity.First(&quota).Error
		}
		return quota, err
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return domains.AccountQuota{}, err
	}
	quota = domains.AccountQuota{
		AccountGuid: input.AccountGuid, WindowType: input.WindowType,
		UsedPercent: input.UsedPercent, LimitWindowSeconds: input.LimitWindowSeconds,
		ResetAt: input.ResetAt, Allowed: input.Allowed, LimitReached: input.LimitReached,
		Source: input.Source, NextRefreshAt: input.NextRefreshAt, LastSyncedAt: input.LastSyncedAt,
		Status: input.Status, Extra: input.Extra,
	}
	return quota, global.NAV_DB.Create(&quota).Error
}

func (s QuotaService) List(params map[string]string) (list interface{}, total int64, err error) {
	limit := commonUtils.Str2Int(params["size"])
	offset := limit * (commonUtils.Str2Int(params["page"]) - 1)
	var results []domains.AccountQuota
	db := global.NAV_DB.Model(new(domains.AccountQuota))
	if params["accountGuid"] != "" {
		db = db.Where("account_guid = ?", params["accountGuid"])
	}
	if params["windowType"] != "" {
		db = db.Where("window_type = ?", params["windowType"])
	}
	if params["status"] != "" {
		db = db.Where("status = ?", params["status"])
	}
	if err = db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err = db.Order("account_guid asc, window_type asc").Limit(limit).Offset(offset).Find(&results).Error
	return results, total, err
}

func (s QuotaService) ListAll(accountGuid string) ([]domains.AccountQuota, error) {
	var list []domains.AccountQuota
	db := global.NAV_DB.Order("account_guid asc, window_type asc")
	if accountGuid != "" {
		db = db.Where("account_guid = ?", accountGuid)
	}
	err := db.Find(&list).Error
	return list, err
}

func (s QuotaService) ListByAccount(accountGuid string) ([]domains.AccountQuota, error) {
	return s.ListAll(accountGuid)
}

// SyncRateLimit 把 wham、普通响应头或主动探测结果写入统一窗口模型。
func (s QuotaService) SyncRateLimit(accountGuid, prefix, source string, limit *chatgpt.RateLimit, raw any) ([]domains.AccountQuota, error) {
	if limit == nil {
		return nil, nil
	}
	extra := ""
	if raw != nil {
		if encoded, err := json.Marshal(raw); err == nil {
			extra = string(encoded)
		}
	}
	inputs := make([]QuotaInput, 0, 2)
	if limit.PrimaryWindow != nil {
		inputs = append(inputs, quotaInputFromWindow(accountGuid, quotaWindowName(prefix, "primary", limit.PrimaryWindow), source, limit, limit.PrimaryWindow, extra))
	}
	if limit.SecondaryWindow != nil {
		inputs = append(inputs, quotaInputFromWindow(accountGuid, quotaWindowName(prefix, "secondary", limit.SecondaryWindow), source, limit, limit.SecondaryWindow, extra))
	}
	out := make([]domains.AccountQuota, 0, len(inputs))
	for _, input := range inputs {
		quota, err := s.Upsert(input)
		if err != nil {
			return out, err
		}
		out = append(out, quota)
	}
	return out, nil
}

// ReconcileSnapshot 在一次完整额度快照同步成功后，移除该来源已经不再返回的旧窗口，
// 并依据窗口的实际数值重算状态。历史版本会在一次额度错误发生时把账号的所有窗口
// 都标记为 exhausted；这里同时负责清除这类与最新额度数据不一致的遗留标记。
func (s QuotaService) ReconcileSnapshot(accountGuid, source string, activeWindowTypes []string) error {
	accountGuid = strings.TrimSpace(accountGuid)
	source = strings.TrimSpace(source)
	if accountGuid == "" || source == "" {
		return errors.New("accountGuid and source are required")
	}

	active := make([]string, 0, len(activeWindowTypes))
	seen := make(map[string]struct{}, len(activeWindowTypes))
	for _, windowType := range activeWindowTypes {
		windowType = strings.TrimSpace(windowType)
		if windowType == "" {
			continue
		}
		if _, ok := seen[windowType]; ok {
			continue
		}
		seen[windowType] = struct{}{}
		active = append(active, windowType)
	}
	if len(active) == 0 {
		return errors.New("quota snapshot contains no active windows")
	}

	return global.NAV_DB.Transaction(func(tx *gorm.DB) error {
		stale := tx.Where("account_guid = ? AND source = ?", accountGuid, source)
		stale = stale.Where("window_type NOT IN ?", active)
		if err := stale.Delete(&domains.AccountQuota{}).Error; err != nil {
			return err
		}

		status := gorm.Expr(
			"CASE WHEN limit_reached = ? OR allowed = ? OR used_percent >= ? THEN ? ELSE ? END",
			true, false, QuotaExhaustedPercentThreshold,
			domains.QuotaStatusExhausted, domains.QuotaStatusAvailable,
		)
		return tx.Model(&domains.AccountQuota{}).
			Where("account_guid = ? AND source = ?", accountGuid, source).
			Update("status", status).Error
	})
}

func (s QuotaService) SampleHeaders(accountGuid, source string, header map[string][]string) ([]domains.AccountQuota, error) {
	snapshot, ok := chatgpt.ParseRateLimitHeaders(header)
	if !ok {
		return nil, nil
	}
	return s.SyncRateLimit(accountGuid, "", source, snapshot.RateLimit, snapshot.Captured)
}

func quotaInputFromWindow(accountGuid, windowType, source string, limit *chatgpt.RateLimit, window *chatgpt.RateLimitWindow, extra string) QuotaInput {
	input := QuotaInput{AccountGuid: accountGuid, WindowType: windowType, Source: source, Extra: extra}
	if window != nil {
		input.UsedPercent = window.UsedPercent
		if window.LimitWindowSeconds != nil {
			input.LimitWindowSeconds = *window.LimitWindowSeconds
		}
		if window.ResetAt != nil {
			input.ResetAt = time.Unix(*window.ResetAt, 0).UnixMilli()
		}
	}
	// allowed/limit_reached in the official /wham/usage response describe the
	// whole rate-limit group, not this individual window. Copying those global
	// flags to every window makes a depleted 5-hour window incorrectly mark the
	// healthy 7-day window as exhausted. Retain the group status only for the
	// sole window, the window whose own usage proves exhaustion, or a primary
	// fallback when the group is exhausted but neither window exposes an
	// exhausted percentage. The complete response remains available in Extra
	// for diagnostics.
	if limit != nil {
		windowCount := 0
		if limit.PrimaryWindow != nil {
			windowCount++
		}
		if limit.SecondaryWindow != nil {
			windowCount++
		}
		windowExhausted := window != nil && window.UsedPercent != nil && *window.UsedPercent >= QuotaExhaustedPercentThreshold
		groupExhausted := limit.LimitReached != nil && *limit.LimitReached || limit.Allowed != nil && !*limit.Allowed
		otherWindowExhausted := false
		if limit.PrimaryWindow != nil && limit.PrimaryWindow != window && limit.PrimaryWindow.UsedPercent != nil {
			otherWindowExhausted = *limit.PrimaryWindow.UsedPercent >= QuotaExhaustedPercentThreshold
		}
		if limit.SecondaryWindow != nil && limit.SecondaryWindow != window && limit.SecondaryWindow.UsedPercent != nil {
			otherWindowExhausted = otherWindowExhausted || *limit.SecondaryWindow.UsedPercent >= QuotaExhaustedPercentThreshold
		}
		// If the group is exhausted but neither window exposes a usable
		// percentage, retain the status on the primary row as a routing fallback.
		// It is still not copied to the other row, so the UI cannot show both as
		// exhausted.
		primaryFallback := windowCount > 1 && groupExhausted && !windowExhausted && !otherWindowExhausted && window == limit.PrimaryWindow
		if windowCount <= 1 || windowExhausted || primaryFallback {
			input.Allowed = limit.Allowed
			input.LimitReached = limit.LimitReached
		}
	}
	return input
}

func quotaWindowName(prefix, kind string, window *chatgpt.RateLimitWindow) string {
	prefix = strings.Trim(strings.TrimSpace(prefix), ":/_- ")
	name := kind
	if window != nil && window.LimitWindowSeconds != nil {
		switch *window.LimitWindowSeconds {
		case int64(5 * time.Hour / time.Second):
			name = "5h"
		case int64(7 * 24 * time.Hour / time.Second):
			name = "7d"
		default:
			name = fmt.Sprintf("%ds", *window.LimitWindowSeconds)
		}
	}
	if prefix == "" {
		return name
	}
	return prefix + ":" + name
}

func (s QuotaService) ApplyError(accountGuid, errorType string) {
	s.ApplyErrorWithPolicy(accountGuid, errorType, AccountFailurePolicy{})
}

func (s QuotaService) ApplyErrorWithPolicy(accountGuid, errorType string, policy AccountFailurePolicy) {
	if accountGuid == "" || !accountFailureRelevant(errorType) {
		return
	}
	_ = AccountServiceApp.MarkFailureWithPolicy(accountGuid, errorType, policy)
}

func (s QuotaService) HasBlockingQuota(accountGuid string) (bool, error) {
	if accountGuid == "" {
		return false, nil
	}
	now := time.Now().UnixMilli()
	var quotas []domains.AccountQuota
	if err := global.NAV_DB.Where("account_guid = ? AND (reset_at = 0 OR reset_at > ?)", accountGuid, now).Find(&quotas).Error; err != nil {
		return false, err
	}
	if quotaSetBlocksRouting(quotas, now) {
		return true, nil
	}
	return false, nil
}

// quotaSetBlocksRouting prefers an official wham snapshot for the same
// window. A response-header or active-probe sample can be used as a fallback
// when no official snapshot exists, but must not override a newer authoritative
// subscription response and make the account appear unavailable in routing.
func quotaSetBlocksRouting(quotas []domains.AccountQuota, now int64) bool {
	officialWindows := make(map[string]struct{})
	for _, quota := range quotas {
		if strings.EqualFold(strings.TrimSpace(quota.Source), "wham") {
			officialWindows[quotaWindowIdentity(quota)] = struct{}{}
		}
	}
	for _, quota := range quotas {
		source := strings.ToLower(strings.TrimSpace(quota.Source))
		if source != "" && source != "wham" {
			if _, official := officialWindows[quotaWindowIdentity(quota)]; official {
				continue
			}
		}
		if quotaBlocksRouting(quota, now) {
			return true
		}
	}
	return false
}

func quotaWindowIdentity(quota domains.AccountQuota) string {
	windowType := strings.ToLower(strings.TrimSpace(quota.WindowType))
	if strings.HasSuffix(windowType, ":5h") || windowType == "5h" {
		return "5h"
	}
	if strings.HasSuffix(windowType, ":7d") || windowType == "7d" {
		return "7d"
	}
	if quota.LimitWindowSeconds > 0 {
		return fmt.Sprintf("seconds:%d", quota.LimitWindowSeconds)
	}
	return "name:" + windowType
}

// quotaBlocksRouting determines whether one persisted quota snapshot should
// remove an account from routing. Non-wham samples are deliberately bounded
// by freshness because they are observational hints rather than an
// authoritative subscription snapshot.
func quotaBlocksRouting(quota domains.AccountQuota, now int64) bool {
	source := strings.ToLower(strings.TrimSpace(quota.Source))
	if source != "" && source != "wham" {
		maxAge := quotaRoutingSampleMaxAge.Milliseconds()
		if quota.LastSyncedAt <= 0 || quota.LastSyncedAt < now-maxAge {
			return false
		}
	}
	if quota.Status == domains.QuotaStatusExhausted || quota.LimitReached != nil && *quota.LimitReached || quota.Allowed != nil && !*quota.Allowed {
		return true
	}
	return quota.UsedPercent != nil && *quota.UsedPercent >= QuotaExhaustedPercentThreshold
}

func (s QuotaService) RecoverCooldownAccounts() error {
	now := time.Now().UnixMilli()
	var accounts []domains.Account
	if err := global.NAV_DB.Where("enabled = ? AND status IN ? AND cooldown_until > 0 AND cooldown_until <= ?", true,
		[]string{domains.AccountStatusLimited, domains.AccountStatusCooldown}, now).Find(&accounts).Error; err != nil {
		return err
	}
	for _, account := range accounts {
		blocked, err := s.HasBlockingQuota(account.Guid)
		if err != nil {
			return err
		}
		updates := map[string]any{"status": domains.AccountStatusAvailable, "cooldown_until": int64(0), "failure_count": 0}
		if blocked {
			updates["status"] = domains.AccountStatusExhausted
			updates["failure_count"] = account.FailureCount
		}
		updates["health_version"] = gorm.Expr("health_version + 1")
		result := global.NAV_DB.Model(&domains.Account{}).
			Where("guid = ? AND health_version = ?", account.Guid, account.HealthVersion).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
	}
	return nil
}

// RefreshExpiredWindows 删除已经失效的采样快照；新的 wham 或响应头会重新创建窗口。
func (s QuotaService) RefreshExpiredWindows(accountGuid string) error {
	query := global.NAV_DB.Where("reset_at > 0 AND reset_at <= ?", time.Now().UnixMilli())
	if accountGuid != "" {
		query = query.Where("account_guid = ?", accountGuid)
	}
	return query.Delete(&domains.AccountQuota{}).Error
}

func normalizeQuotaInput(input QuotaInput) QuotaInput {
	now := time.Now().UnixMilli()
	if input.UsedPercent != nil {
		value := *input.UsedPercent
		if value < 0 {
			value = 0
		}
		if value > 100 {
			value = 100
		}
		input.UsedPercent = &value
	}
	if input.Status == "" {
		input.Status = domains.QuotaStatusAvailable
		if input.LimitReached != nil && *input.LimitReached || input.Allowed != nil && !*input.Allowed || input.UsedPercent != nil && *input.UsedPercent >= QuotaExhaustedPercentThreshold {
			input.Status = domains.QuotaStatusExhausted
		}
	}
	if input.LastSyncedAt == 0 {
		input.LastSyncedAt = now
	}
	if input.NextRefreshAt == 0 {
		refreshEvery := Config().QuotaRefreshSeconds
		if refreshEvery <= 0 {
			refreshEvery = 180
		}
		input.NextRefreshAt = time.Now().Add(time.Duration(refreshEvery) * time.Second).UnixMilli()
	}
	return input
}
