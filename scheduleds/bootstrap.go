package scheduleds

import (
	"freeai/services"
	fmgutils "freeai/utils"
)

func Bootstrap() {
	_ = services.AccountGroupServiceApp.EnsureDefaults()
	_ = services.QuotaServiceApp.RecoverCooldownAccounts()
	_ = services.QuotaServiceApp.RefreshExpiredWindows("")
	_ = services.AccountServiceApp.MarkExpiredSubscriptions()
	_ = fmgutils.CheckMasterKey(services.Config().SecretKeyFile)
}
