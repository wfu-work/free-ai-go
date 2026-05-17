package scheduleds

import "freeai/services"

func Bootstrap() {
	_ = services.QuotaServiceApp.RecoverCooldownAccounts()
}
