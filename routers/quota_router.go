package routers

import (
	"github.com/gin-gonic/gin"
)

type QuotaRouter struct{}

func (r QuotaRouter) InitQuotaRouter(group *gin.RouterGroup) {
	{
		group.GET("quotas/list", quotaApi.List)
		group.GET("quotas/list/all", quotaApi.ListAll)
		group.GET("accounts/:guid/quotas", quotaApi.ListByAccount)
	}
}
