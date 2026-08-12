package domains

import (
	"os"

	"github.com/wfu-work/nav-common-go-lib/global"
	"go.uber.org/zap"
)

func RegisterTables() {
	db := global.NAV_DB
	if err := db.AutoMigrate(
		Account{},
		AccountGroup{},
		AccountQuota{},
		AccountResetCreditRedemption{},
		ModelCatalog{},
		AccountModelAvailability{},
		ModelExposure{},
		ModelPricing{},
		PlatformKey{},
		PlatformKeyReservation{},
		RequestLog{},
		RouteState{},
		AuditLog{},
		SystemConfig{},
	); err != nil {
		global.NAV_LOG.Error("register FreeAiGo tables failed", zap.Error(err))
		os.Exit(1)
	}
	global.NAV_LOG.Info("register FreeAiGo tables success")
}
