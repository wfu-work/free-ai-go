package routers

import (
	"github.com/gin-gonic/gin"
	"github.com/wfu-work/nav-common-go-lib/middlewares"
)

type QuotaRouter struct{}

func (r QuotaRouter) InitQuotaRouter(group *gin.RouterGroup) {
	routerLogger := group.Group("").Use(middlewares.ApiLogger())
	{
		group.GET("quotas/list", quotaApi.List)
		group.GET("quotas/list/all", quotaApi.ListAll)
		group.GET("accounts/:guid/quotas", quotaApi.ListByAccount)
		routerLogger.POST("accounts/:guid/quotas", quotaApi.Upsert)
	}
}
