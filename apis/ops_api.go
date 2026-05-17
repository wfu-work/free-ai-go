package apis

import (
	"freeai/domains"

	"github.com/gin-gonic/gin"
	"github.com/wfu-work/nav-common-go-lib/global"
	"github.com/wfu-work/nav-common-go-lib/response"
)

type OpsApi struct{}

func (a OpsApi) Metrics(c *gin.Context) {
	var accounts int64
	var availableAccounts int64
	var models int64
	var platformKeys int64
	_ = global.NAV_DB.Model(&domains.Account{}).Count(&accounts).Error
	_ = global.NAV_DB.Model(&domains.Account{}).Where("enabled = ? AND status = ?", true, domains.AccountStatusAvailable).Count(&availableAccounts).Error
	_ = global.NAV_DB.Model(&domains.ModelMapping{}).Where("enabled = ?", true).Count(&models).Error
	_ = global.NAV_DB.Model(&domains.PlatformKey{}).Where("enabled = ?", true).Count(&platformKeys).Error
	response.Ok(gin.H{
		"ok":                true,
		"name":              "FreeAiGo",
		"accounts":          accounts,
		"availableAccounts": availableAccounts,
		"enabledModels":     models,
		"enabledKeys":       platformKeys,
	}, c)
}

func (a OpsApi) Stats(c *gin.Context) {
	stats, err := requestLogService.Stats()
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(stats, c)
}

func (a OpsApi) Routes(c *gin.Context) {
	var routes []domains.RouteState
	if err := global.NAV_DB.Order("updated_at_unix desc").Limit(200).Find(&routes).Error; err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(routes, c)
}
