package apis

import (
	"freeai/domains"
	"freeai/services"
	fmgutils "freeai/utils"

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

func (a OpsApi) AccountHealth(c *gin.Context) {
	var accounts []domains.Account
	if err := global.NAV_DB.Order("provider asc, account_group asc, priority asc, id desc").Find(&accounts).Error; err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	guids := make([]string, 0, len(accounts))
	for _, account := range accounts {
		guids = append(guids, account.Guid)
	}
	var quotas []domains.AccountQuota
	if len(guids) > 0 {
		if err := global.NAV_DB.Where("account_guid IN ?", guids).Find(&quotas).Error; err != nil {
			response.FailWithMessage(err.Error(), c)
			return
		}
	}
	quotaByAccount := map[string][]domains.AccountQuota{}
	for _, quota := range quotas {
		quotaByAccount[quota.AccountGuid] = append(quotaByAccount[quota.AccountGuid], quota)
	}
	items := make([]gin.H, 0, len(accounts))
	for _, account := range accounts {
		items = append(items, gin.H{
			"guid":                  account.Guid,
			"name":                  account.Name,
			"provider":              account.Provider,
			"accountGroup":          account.AccountGroup,
			"status":                account.Status,
			"enabled":               account.Enabled,
			"failureCount":          account.FailureCount,
			"cooldownUntil":         account.CooldownUntil,
			"lastUsedAt":            account.LastUsedAt,
			"subscriptionExpiredAt": account.SubscriptionExpiredAt,
			"quotas":                quotaByAccount[account.Guid],
		})
	}
	response.Ok(items, c)
}

func (a OpsApi) MasterKey(c *gin.Context) {
	response.Ok(fmgutils.CheckMasterKey(services.Config().SecretKeyFile), c)
}
