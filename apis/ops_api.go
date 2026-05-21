package apis

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/wfu-work/free-ai-go/domains"
	"github.com/wfu-work/free-ai-go/services"
	"github.com/wfu-work/free-ai-go/utils"
	"github.com/wfu-work/nav-common-go-lib/global"
	"github.com/wfu-work/nav-common-go-lib/response"
)

type OpsApi struct{}

// Metrics 获取运维指标
// @Summary 获取运维指标
// @Description 获取运维指标
// @Tags 运维模块
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Success 200 {object} response.Response{data=object,msg=string}
// @Router /ops/metrics [get]
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
		"proxyPrefix":       services.Config().ProxyPrefix,
		"accounts":          accounts,
		"availableAccounts": availableAccounts,
		"enabledModels":     models,
		"enabledKeys":       platformKeys,
	}, c)
}

// GatewayConfig 获取网关配置
// @Summary 获取网关配置
// @Description 获取网关配置
// @Tags 运维模块
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Success 200 {object} response.Response{data=object,msg=string}
// @Router /ops/gateway-config [get]
func (a OpsApi) GatewayConfig(c *gin.Context) {
	response.Ok(services.GatewayProxyConfig(), c)
}

// SaveGatewayConfig 保存网关配置
// @Summary 保存网关配置
// @Description 保存网关配置
// @Tags 运维模块
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Success 200 {object} response.Response{data=object,msg=string}
// @Router /ops/gateway-config [put]
func (a OpsApi) SaveGatewayConfig(c *gin.Context) {
	var input services.GatewayProxyConfigInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	cfg, err := services.UpdateGatewayProxyConfig(input)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(cfg, c)
}

// Stats 获取请求统计
// @Summary 获取请求统计
// @Description 获取请求统计
// @Tags 运维模块
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Success 200 {object} response.Response{data=object,msg=string}
// @Router /ops/stats [get]
func (a OpsApi) Stats(c *gin.Context) {
	stats, err := requestLogService.Stats()
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(stats, c)
}

// Routes 获取路由状态
// @Summary 获取路由状态
// @Description 获取路由状态
// @Tags 运维模块
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Success 200 {object} response.Response{data=[]domains.RouteState,msg=string}
// @Router /ops/routes [get]
func (a OpsApi) Routes(c *gin.Context) {
	var routes []domains.RouteState
	if err := global.NAV_DB.Order("updated_at_unix desc").Limit(200).Find(&routes).Error; err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(routes, c)
}

// AccountHealth 获取账号健康度
// @Summary 获取账号健康度
// @Description 获取账号健康度
// @Tags 运维模块
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Success 200 {object} response.Response{data=[]object,msg=string}
// @Router /ops/account-health [get]
func (a OpsApi) AccountHealth(c *gin.Context) {
	now := time.Now().UnixMilli()
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
		accountQuotas := quotaByAccount[account.Guid]
		effectiveStatus := account.Status
		if !account.Enabled {
			effectiveStatus = domains.AccountStatusDisabled
		} else {
			if hasBlockingQuotaSnapshot(accountQuotas, now) {
				effectiveStatus = domains.AccountStatusExhausted
			} else if effectiveStatus == "" || effectiveStatus == domains.AccountStatusUnknown {
				effectiveStatus = domains.AccountStatusAvailable
			}
		}
		items = append(items, gin.H{
			"guid":                  account.Guid,
			"name":                  account.Name,
			"provider":              account.Provider,
			"supplierName":          account.SupplierName,
			"usageQueryType":        account.UsageQueryType,
			"usageApiUrl":           account.UsageAPIURL,
			"accountGroup":          account.AccountGroup,
			"status":                effectiveStatus,
			"enabled":               account.Enabled,
			"failureCount":          account.FailureCount,
			"cooldownUntil":         account.CooldownUntil,
			"lastUsedAt":            account.LastUsedAt,
			"subscriptionExpiredAt": account.SubscriptionExpiredAt,
			"nextUsageCheckAt":      nextUsageCheckAt(account, accountQuotas),
			"quotas":                accountQuotas,
		})
	}
	response.Ok(items, c)
}

func nextUsageCheckAt(account domains.Account, quotas []domains.AccountQuota) int64 {
	if !supportsAccountUsageQuery(account) {
		return 0
	}
	if len(quotas) == 0 {
		return 0
	}
	var next int64
	for _, quota := range quotas {
		if quota.NextRefreshAt <= 0 {
			return 0
		}
		if next == 0 || quota.NextRefreshAt < next {
			next = quota.NextRefreshAt
		}
	}
	return next
}

func supportsAccountUsageQuery(account domains.Account) bool {
	if strings.TrimSpace(account.UsageQueryType) == "codexzh" || strings.EqualFold(account.Provider, "codexzh") {
		return true
	}
	if strings.TrimSpace(account.UsageQueryType) != "" {
		return false
	}
	values := []string{account.SupplierName, account.OfficialURL, account.APIBaseURL, account.UsageAPIURL}
	for _, value := range values {
		if strings.Contains(strings.ToLower(strings.TrimSpace(value)), "codexzh") {
			return true
		}
	}
	return false
}

func hasBlockingQuotaSnapshot(quotas []domains.AccountQuota, now int64) bool {
	for _, quota := range quotas {
		if quota.ResetAt > 0 && quota.ResetAt <= now {
			continue
		}
		if quota.Status == domains.QuotaStatusExhausted {
			return true
		}
		if quota.TotalAmount > 0 && (quota.RemainingAmount <= 0 || quota.UsedPercent >= services.QuotaExhaustedPercentThreshold) {
			return true
		}
		if quota.TotalTokens > 0 && (quota.RemainingTokens <= 0 || quota.UsedPercent >= services.QuotaExhaustedPercentThreshold) {
			return true
		}
	}
	return false
}

// MasterKey 获取主密钥状态
// @Summary 获取主密钥状态
// @Description 获取主密钥状态
// @Tags 运维模块
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Success 200 {object} response.Response{data=utils.MasterKeyStatus,msg=string}
// @Router /ops/master-key [get]
func (a OpsApi) MasterKey(c *gin.Context) {
	response.Ok(utils.CheckMasterKey(services.Config().SecretKeyFile), c)
}

// ExportCoreBackup 导出核心数据备份
// @Summary 导出核心数据备份
// @Description 导出核心数据备份
// @Tags 运维模块
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Success 200 {object} services.CoreBackupPayload
// @Router /ops/core-backup [get]
func (a OpsApi) ExportCoreBackup(c *gin.Context) {
	payload, err := services.BackupServiceApp.ExportCore()
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	filename := fmt.Sprintf("freeai-core-backup-%s.json", time.Now().Format("20060102-150405"))
	c.Header("Content-Type", "application/json; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Data(200, "application/json; charset=utf-8", body)
}

// ImportCoreBackup 导入核心数据备份
// @Summary 导入核心数据备份
// @Description 导入核心数据备份
// @Tags 运维模块
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Success 200 {object} response.Response{data=services.CoreBackupImportResult,msg=string}
// @Router /ops/core-backup [post]
func (a OpsApi) ImportCoreBackup(c *gin.Context) {
	var payload services.CoreBackupPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	result, err := services.BackupServiceApp.ImportCore(payload)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(result, c)
}
