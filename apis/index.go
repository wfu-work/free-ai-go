package apis

import "freeai/services"

var ApiGroupApp = new(ApiGroup)

type ApiGroup struct {
	AccountApi
	AccountGroupApi
	PlatformKeyApi
	ModelApi
	QuotaApi
	RequestLogApi
	OpsApi
	ProxyApi
}

var (
	accountService      = services.AccountServiceApp
	accountGroupService = services.AccountGroupServiceApp
	platformKeyService  = services.PlatformKeyServiceApp
	modelService        = services.ModelServiceApp
	quotaService        = services.QuotaServiceApp
	requestLogService   = services.RequestLogServiceApp
	proxyService        = services.ProxyServiceApp
)
