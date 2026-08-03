package scheduleds

import (
	"context"
	"fmt"

	"github.com/robfig/cron/v3"
	"github.com/wfu-work/free-ai-go/services"
	"github.com/wfu-work/free-ai-go/utils"
	"github.com/wfu-work/nav-common-go-lib/global"
	commonscheduleds "github.com/wfu-work/nav-common-go-lib/scheduleds"
	"go.uber.org/zap"
)

func Register(timers commonscheduleds.Timer, options []cron.Option) {
	cfg := services.Config()
	cooldownSpec := fmt.Sprintf("@every %ds", cfg.CooldownSeconds)
	usageSpec := fmt.Sprintf("@every %ds", cfg.QuotaRefreshSeconds)
	modelSpec := "@every 6h"
	cleanupSpec := "@daily"
	if cfg.CooldownSeconds <= 0 {
		cooldownSpec = "@every 300s"
	}
	if cfg.QuotaRefreshSeconds <= 0 {
		usageSpec = "@every 180s"
	}
	_, _ = timers.AddTaskByFunc("freeai", cooldownSpec, func() {
		if err := services.QuotaServiceApp.RecoverCooldownAccounts(); err != nil {
			global.NAV_LOG.Warn("recover cooldown accounts failed", zap.Error(err))
		}
		if err := services.QuotaServiceApp.RefreshExpiredWindows(""); err != nil {
			global.NAV_LOG.Warn("refresh expired quota windows failed", zap.Error(err))
		}
		if err := services.AccountServiceApp.MarkExpiredSubscriptions(); err != nil {
			global.NAV_LOG.Warn("mark expired subscriptions failed", zap.Error(err))
		}
		if status := utils.CheckMasterKey(cfg.SecretKeyFile); !status.Loaded {
			global.NAV_LOG.Warn("master key check failed", zap.String("path", status.Path), zap.String("error", status.Error))
		}
	}, "recover-cooldown-accounts", options...)
	_, _ = timers.AddTaskByFunc("freeai", usageSpec, func() {
		result, err := services.AccountServiceApp.RefreshDueUsageAccounts()
		if err != nil {
			global.NAV_LOG.Warn("refresh account pool usage failed", zap.Error(err))
			return
		}
		if result.Failed > 0 {
			global.NAV_LOG.Warn("some account usage refreshes failed", zap.Int("checked", result.Checked), zap.Int("updated", result.Updated), zap.Int("failed", result.Failed))
		}
	}, "refresh-account-pool-usage", options...)
	_, _ = timers.AddTaskByFunc("freeai", modelSpec, func() {
		result, err := services.AccountServiceApp.SyncModels(services.ModelSyncInput{})
		if err != nil {
			global.NAV_LOG.Warn("sync official model catalog failed", zap.Error(err))
			return
		}
		if result.Failed > 0 {
			global.NAV_LOG.Warn("some model catalog syncs failed", zap.Int("checked", result.Checked), zap.Int("updated", result.Updated), zap.Int("failed", result.Failed))
		}
	}, "sync-official-model-catalog", options...)
	_, _ = timers.AddTaskByFunc("freeai", modelSpec, func() {
		result, err := services.ModelPricingServiceApp.SyncOfficial(context.Background())
		if err != nil {
			global.NAV_LOG.Warn("sync official model pricing failed", zap.Error(err))
			return
		}
		if result.Warning != "" {
			global.NAV_LOG.Warn("official model pricing synced with fallback", zap.String("source", result.SourceKind), zap.String("warning", result.Warning))
		}
	}, "sync-official-model-pricing", options...)
	_, _ = timers.AddTaskByFunc("freeai", cleanupSpec, func() {
		if err := services.RequestLogServiceApp.CleanupExpired(cfg.CleanupLogRetentionDays); err != nil {
			global.NAV_LOG.Warn("cleanup request logs failed", zap.Error(err))
		}
	}, "cleanup-request-logs", options...)
}
