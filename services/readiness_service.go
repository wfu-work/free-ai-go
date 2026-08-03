package services

import (
	"context"

	"github.com/wfu-work/free-ai-go/domains"
	"github.com/wfu-work/nav-common-go-lib/global"
)

// ReadinessStatus 描述网关是否已经具备承接真实 API 请求的必要条件。
type ReadinessStatus struct {
	Ready             bool     `json:"ready"`
	Database          bool     `json:"database"`
	AvailableAccounts int      `json:"availableAccounts"`
	EnabledModels     int      `json:"enabledModels"`
	EnabledKeys       int      `json:"enabledKeys"`
	RoutablePairs     int      `json:"routablePairs"`
	Reasons           []string `json:"reasons"`
}

// Readiness 检查数据库、官方账号、模型目录、平台密钥和实际路由组合。
func Readiness(ctx context.Context) ReadinessStatus {
	status := ReadinessStatus{Reasons: make([]string, 0)}
	if global.NAV_DB == nil {
		status.Reasons = append(status.Reasons, "database is not initialized")
		return status
	}
	sqlDB, err := global.NAV_DB.DB()
	if err != nil || sqlDB.PingContext(ctx) != nil {
		status.Reasons = append(status.Reasons, "database is unavailable")
		return status
	}
	status.Database = true
	accounts, err := AccountServiceApp.FindAvailable("", "", 1000)
	if err != nil {
		status.Reasons = append(status.Reasons, "account pool check failed")
	} else {
		status.AvailableAccounts = len(accounts)
	}
	models, err := ModelServiceApp.ListEnabled()
	if err != nil {
		status.Reasons = append(status.Reasons, "model catalog check failed")
	} else {
		status.EnabledModels = len(models)
	}
	var keys []domains.PlatformKey
	if err := global.NAV_DB.Where("enabled = ?", true).Find(&keys).Error; err != nil {
		status.Reasons = append(status.Reasons, "platform key check failed")
	} else {
		status.EnabledKeys = len(keys)
	}
	if len(accounts) == 0 {
		status.Reasons = append(status.Reasons, "no available official account")
	}
	if len(models) == 0 {
		status.Reasons = append(status.Reasons, "no enabled model")
	}
	if len(keys) == 0 {
		status.Reasons = append(status.Reasons, "no enabled platform key")
	}
	for _, key := range keys {
		for _, model := range models {
			if PlatformKeyServiceApp.ModelExposureAllowed(key, model) && RouterServiceApp.HasAvailableAccount(model, key) {
				status.RoutablePairs++
			}
		}
	}
	if status.RoutablePairs == 0 && len(keys) > 0 && len(models) > 0 {
		status.Reasons = append(status.Reasons, "no routable key and model combination")
	}
	status.Ready = status.Database && status.AvailableAccounts > 0 && status.EnabledModels > 0 && status.EnabledKeys > 0 && status.RoutablePairs > 0
	return status
}
