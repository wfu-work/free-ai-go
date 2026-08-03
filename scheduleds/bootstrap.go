package scheduleds

import (
	"context"

	"github.com/wfu-work/free-ai-go/services"
	"github.com/wfu-work/free-ai-go/utils"
	"github.com/wfu-work/nav-common-go-lib/global"
	"go.uber.org/zap"
)

func Bootstrap() {
	_ = services.AccountGroupServiceApp.EnsureDefaults()
	_ = services.QuotaServiceApp.RecoverCooldownAccounts()
	_ = services.QuotaServiceApp.RefreshExpiredWindows("")
	_ = services.AccountServiceApp.MarkExpiredSubscriptions()
	_ = services.PlatformKeyServiceApp.ReconcileUsageFromLogs()
	_ = utils.CheckMasterKey(services.Config().SecretKeyFile)
	go func() { _, _ = services.AccountServiceApp.RefreshDueUsageAccounts() }()
	go func() { _, _ = services.AccountServiceApp.SyncModels(services.ModelSyncInput{}) }()
	go func() {
		result, err := services.ModelPricingServiceApp.SyncOfficial(context.Background())
		if err != nil {
			global.NAV_LOG.Warn("bootstrap official model pricing failed", zap.Error(err))
			return
		}
		if result.Warning != "" {
			global.NAV_LOG.Warn("bootstrap model pricing used fallback", zap.String("source", result.SourceKind), zap.String("warning", result.Warning))
		}
	}()
}
