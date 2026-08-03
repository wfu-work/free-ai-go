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

func (s QuotaService) Upsert(input QuotaInput) (domains.AccountQuota, error) {
	input.AccountGuid = strings.TrimSpace(input.AccountGuid)
	input.WindowType = strings.TrimSpace(input.WindowType)
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
	err := global.NAV_DB.Where("account_guid = ? AND window_type = ?", input.AccountGuid, input.WindowType).First(&quota).Error
	if err == nil {
		err = global.NAV_DB.Model(&quota).Updates(updates).Error
		if err == nil {
			_ = global.NAV_DB.Where("account_guid = ? AND window_type = ?", input.AccountGuid, input.WindowType).First(&quota).Error
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
	if limit != nil {
		input.Allowed = limit.Allowed
		input.LimitReached = limit.LimitReached
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
	if accountGuid == "" || errorType == "" {
		return
	}
	_ = AccountServiceApp.MarkFailure(accountGuid, errorType)
	if errorType != domains.ErrorRateLimited && errorType != domains.ErrorQuotaExhausted {
		return
	}
	status := domains.QuotaStatusLimited
	if errorType == domains.ErrorQuotaExhausted {
		status = domains.QuotaStatusExhausted
	}
	_ = global.NAV_DB.Model(&domains.AccountQuota{}).Where("account_guid = ?", accountGuid).Update("status", status).Error
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
	for _, quota := range quotas {
		if quota.Status == domains.QuotaStatusExhausted || quota.LimitReached != nil && *quota.LimitReached || quota.Allowed != nil && !*quota.Allowed {
			return true, nil
		}
		if quota.UsedPercent != nil && *quota.UsedPercent >= QuotaExhaustedPercentThreshold {
			return true, nil
		}
	}
	return false, nil
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
		if err := global.NAV_DB.Model(&account).Updates(updates).Error; err != nil {
			return err
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
