package routers

import (
	"freeai/apis"

	"github.com/gin-gonic/gin"
)

var RouterGroupApp = new(RouterGroup)

type RouterGroup struct {
	HealthRouter
	AccountRouter
	AccountGroupRouter
	PlatformKeyRouter
	ModelRouter
	QuotaRouter
	RequestLogRouter
	OpsRouter
	ProxyRouter
}

var (
	accountApi      = apis.ApiGroupApp.AccountApi
	accountGroupApi = apis.ApiGroupApp.AccountGroupApi
	platformKeyApi  = apis.ApiGroupApp.PlatformKeyApi
	modelApi        = apis.ApiGroupApp.ModelApi
	quotaApi        = apis.ApiGroupApp.QuotaApi
	requestLogApi   = apis.ApiGroupApp.RequestLogApi
	opsApi          = apis.ApiGroupApp.OpsApi
	proxyApi        = apis.ApiGroupApp.ProxyApi
)

func (r *RouterGroup) InitFreeModelRouters(publicGroup *gin.RouterGroup, privateGroup *gin.RouterGroup) {
	r.InitHealthRouter(publicGroup)
	r.InitAccountRouter(privateGroup)
	r.InitAccountGroupRouter(privateGroup)
	r.InitPlatformKeyRouter(privateGroup)
	r.InitModelRouter(privateGroup)
	r.InitQuotaRouter(privateGroup)
	r.InitRequestLogRouter(privateGroup)
	r.InitOpsRouter(privateGroup)
}

func (r *RouterGroup) InitProxyWebRouter(engine *gin.Engine) {
	r.InitProxyRouter(engine)
}
