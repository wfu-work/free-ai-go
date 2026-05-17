package services

import (
	"time"

	"freeai/domains"

	"github.com/wfu-work/nav-common-go-lib/global"
)

type QuotaService struct{}

var QuotaServiceApp = QuotaService{}

type QuotaInput struct {
	AccountGuid     string  `json:"accountGuid"`
	WindowType      string  `json:"windowType"`
	UsedPercent     float64 `json:"usedPercent"`
	RemainingTokens int64   `json:"remainingTokens"`
	TotalTokens     int64   `json:"totalTokens"`
	ResetAt         int64   `json:"resetAt"`
	NextRefreshAt   int64   `json:"nextRefreshAt"`
	Status          string  `json:"status"`
}

func (s QuotaService) Upsert(input QuotaInput) (domains.AccountQuota, error) {
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
			"reset_at":         input.ResetAt,
			"next_refresh_at":  input.NextRefreshAt,
			"status":           input.Status,
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
		ResetAt:         input.ResetAt,
		NextRefreshAt:   input.NextRefreshAt,
		Status:          input.Status,
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
