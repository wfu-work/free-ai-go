package services

import (
	"errors"
	"time"

	"freeai/domains"

	"github.com/wfu-work/nav-common-go-lib/global"
	"gorm.io/gorm"
)

type QuotaService struct{}

var QuotaServiceApp = QuotaService{}

type QuotaInput struct {
	AccountGuid     string  `json:"accountGuid"`
	WindowType      string  `json:"windowType"`
	UsedPercent     float64 `json:"usedPercent"`
	RemainingTokens int64   `json:"remainingTokens"`
	TotalTokens     int64   `json:"totalTokens"`
	Unit            string  `json:"unit"`
	UsedAmount      float64 `json:"usedAmount"`
	RemainingAmount float64 `json:"remainingAmount"`
	TotalAmount     float64 `json:"totalAmount"`
	ResetAt         int64   `json:"resetAt"`
	NextRefreshAt   int64   `json:"nextRefreshAt"`
	LastSyncedAt    int64   `json:"lastSyncedAt"`
	Status          string  `json:"status"`
	Extra           string  `json:"extra"`
}

func (s QuotaService) Upsert(input QuotaInput) (domains.AccountQuota, error) {
	if input.AccountGuid == "" {
		return domains.AccountQuota{}, errors.New("accountGuid is required")
	}
	if input.WindowType == "" {
		return domains.AccountQuota{}, errors.New("windowType is required")
	}
	input = normalizeQuotaInput(input)
	if input.Status == "" {
		input.Status = domains.QuotaStatusUnknown
	}
	var quota domains.AccountQuota
	err := global.NAV_DB.Where("account_guid = ? AND window_type = ?", input.AccountGuid, input.WindowType).First(&quota).Error
	if err == nil {
		updates := map[string]any{
			"used_percent":     input.UsedPercent,
			"remaining_tokens": input.RemainingTokens,
			"total_tokens":     input.TotalTokens,
			"unit":             input.Unit,
			"used_amount":      input.UsedAmount,
			"remaining_amount": input.RemainingAmount,
			"total_amount":     input.TotalAmount,
			"reset_at":         input.ResetAt,
			"next_refresh_at":  input.NextRefreshAt,
			"last_synced_at":   input.LastSyncedAt,
			"status":           input.Status,
			"extra":            input.Extra,
		}
		err = global.NAV_DB.Model(&quota).Updates(updates).Error
		return quota, err
	}
	quota = domains.AccountQuota{
		AccountGuid:     input.AccountGuid,
		WindowType:      input.WindowType,
		UsedPercent:     input.UsedPercent,
		RemainingTokens: input.RemainingTokens,
		TotalTokens:     input.TotalTokens,
		Unit:            input.Unit,
		UsedAmount:      input.UsedAmount,
		RemainingAmount: input.RemainingAmount,
		TotalAmount:     input.TotalAmount,
		ResetAt:         input.ResetAt,
		NextRefreshAt:   input.NextRefreshAt,
		LastSyncedAt:    input.LastSyncedAt,
		Status:          input.Status,
		Extra:           input.Extra,
	}
	err = global.NAV_DB.Create(&quota).Error
	return quota, err
}

func (s QuotaService) List(accountGuid string) ([]domains.AccountQuota, error) {
	var list []domains.AccountQuota
	query := global.NAV_DB.Order("id desc")
	if accountGuid != "" {
		query = query.Where("account_guid = ?", accountGuid)
	}
	err := query.Find(&list).Error
	return list, err
}

func (s QuotaService) ApplyError(accountGuid, errorType string) {
	if accountGuid == "" || errorType == "" {
		return
	}
	_ = AccountServiceApp.MarkFailure(accountGuid, errorType)
	quotaStatus := ""
	switch errorType {
	case domains.ErrorRateLimited:
		quotaStatus = domains.QuotaStatusLimited
	case domains.ErrorQuotaExhausted:
		quotaStatus = domains.QuotaStatusExhausted
	}
	if quotaStatus != "" {
		_ = global.NAV_DB.Model(&domains.AccountQuota{}).
			Where("account_guid = ?", accountGuid).
			Update("status", quotaStatus).Error
	}
}

func (s QuotaService) ApplyUsage(accountGuid string, inputTokens, outputTokens int64) {
	if accountGuid == "" || inputTokens+outputTokens <= 0 {
		return
	}
	var quotas []domains.AccountQuota
	if err := global.NAV_DB.Where("account_guid = ?", accountGuid).Find(&quotas).Error; err != nil {
		return
	}
	used := inputTokens + outputTokens
	for _, quota := range quotas {
		updates := map[string]any{}
		if quota.RemainingTokens > 0 {
			remaining := quota.RemainingTokens - used
			if remaining < 0 {
				remaining = 0
			}
			updates["remaining_tokens"] = remaining
			if quota.TotalTokens > 0 {
				updates["used_percent"] = float64(quota.TotalTokens-remaining) / float64(quota.TotalTokens) * 100
			}
			if remaining == 0 {
				updates["status"] = domains.QuotaStatusExhausted
			}
		}
		if len(updates) > 0 {
			_ = global.NAV_DB.Model(&quota).Updates(updates).Error
		}
	}
}

func (s QuotaService) RecoverCooldownAccounts() error {
	now := time.Now().UnixMilli()
	return global.NAV_DB.Model(&domains.Account{}).
		Where("enabled = ? AND status IN ? AND cooldown_until > 0 AND cooldown_until <= ?", true, []string{domains.AccountStatusLimited, domains.AccountStatusCooldown}, now).
		Updates(map[string]any{
			"status":         domains.AccountStatusAvailable,
			"cooldown_until": int64(0),
			"failure_count":  0,
		}).Error
}

func (s QuotaService) RefreshExpiredWindows(accountGuid string) error {
	now := time.Now().UnixMilli()
	query := global.NAV_DB.Model(&domains.AccountQuota{}).
		Where("reset_at > 0 AND reset_at <= ?", now)
	if accountGuid != "" {
		query = query.Where("account_guid = ?", accountGuid)
	}
	return query.Updates(map[string]any{
		"used_percent":     float64(0),
		"remaining_tokens": gorm.Expr("total_tokens"),
		"status":           domains.QuotaStatusAvailable,
		"reset_at":         int64(0),
	}).Error
}

func normalizeQuotaInput(input QuotaInput) QuotaInput {
	now := time.Now().UnixMilli()
	if input.Unit == "" {
		input.Unit = "token"
	}
	if input.TotalTokens > 0 && input.RemainingTokens == 0 && input.UsedPercent == 0 {
		input.RemainingTokens = input.TotalTokens
	}
	if input.TotalTokens > 0 && input.RemainingTokens >= 0 {
		used := input.TotalTokens - input.RemainingTokens
		if used < 0 {
			used = 0
		}
		input.UsedPercent = float64(used) / float64(input.TotalTokens) * 100
	}
	if input.Status == "" {
		switch {
		case input.TotalAmount > 0 && input.RemainingAmount <= 0:
			input.Status = domains.QuotaStatusExhausted
		case input.TotalTokens > 0 && input.RemainingTokens == 0:
			input.Status = domains.QuotaStatusExhausted
		default:
			input.Status = domains.QuotaStatusAvailable
		}
	}
	if input.TotalAmount > 0 {
		if input.RemainingAmount < 0 {
			input.RemainingAmount = 0
		}
		if input.UsedAmount == 0 {
			input.UsedAmount = input.TotalAmount - input.RemainingAmount
		}
		if input.UsedAmount < 0 {
			input.UsedAmount = 0
		}
		input.UsedPercent = input.UsedAmount / input.TotalAmount * 100
	}
	if input.ResetAt == 0 {
		input.ResetAt = defaultQuotaResetAt(input.WindowType, now)
	}
	if input.NextRefreshAt == 0 {
		refreshEvery := Config().QuotaRefreshSeconds
		if refreshEvery <= 0 {
			refreshEvery = 300
		}
		input.NextRefreshAt = time.Now().Add(time.Duration(refreshEvery) * time.Second).UnixMilli()
	}
	if input.LastSyncedAt == 0 {
		input.LastSyncedAt = now
	}
	return input
}

func defaultQuotaResetAt(windowType string, nowMs int64) int64 {
	now := time.UnixMilli(nowMs)
	switch windowType {
	case "5h", "5_hour", "five_hour":
		return now.Add(5 * time.Hour).UnixMilli()
	case "7d", "7_day", "weekly":
		return now.Add(7 * 24 * time.Hour).UnixMilli()
	case "daily", "day":
		return time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location()).UnixMilli()
	case "monthly", "month":
		return time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, now.Location()).UnixMilli()
	default:
		return 0
	}
}
