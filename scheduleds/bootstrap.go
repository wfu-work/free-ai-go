package scheduleds

import (
	"github.com/wfu-work/free-ai-go/services"
	"github.com/wfu-work/free-ai-go/utils"
)

func Bootstrap() {
	_ = services.AccountGroupServiceApp.EnsureDefaults()
	_ = services.QuotaServiceApp.RecoverCooldownAccounts()
	_ = services.QuotaServiceApp.RefreshExpiredWindows("")
	_ = services.AccountServiceApp.MarkExpiredSubscriptions()
	_ = utils.CheckMasterKey(services.Config().SecretKeyFile)
}
