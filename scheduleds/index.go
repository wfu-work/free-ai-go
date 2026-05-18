package scheduleds

import (
	"fmt"

	"freeai/services"
	fmgutils "freeai/utils"

	"github.com/robfig/cron/v3"
	"github.com/wfu-work/nav-common-go-lib/global"
	commonscheduleds "github.com/wfu-work/nav-common-go-lib/scheduleds"
	"go.uber.org/zap"
)

func Register(timers commonscheduleds.Timer, options []cron.Option) {
	cfg := services.Config()
	cooldownSpec := fmt.Sprintf("@every %ds", cfg.CooldownSeconds)
	cleanupSpec := "@daily"
	if cfg.CooldownSeconds <= 0 {
		cooldownSpec = "@every 300s"
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
		if status := fmgutils.CheckMasterKey(cfg.SecretKeyFile); !status.Loaded {
			global.NAV_LOG.Warn("master key check failed", zap.String("path", status.Path), zap.String("error", status.Error))
		}
	}, "recover-cooldown-accounts", options...)
	_, _ = timers.AddTaskByFunc("freeai", cleanupSpec, func() {
		if err := services.RequestLogServiceApp.CleanupExpired(cfg.CleanupLogRetentionDays); err != nil {
			global.NAV_LOG.Warn("cleanup request logs failed", zap.Error(err))
		}
	}, "cleanup-request-logs", options...)
}
