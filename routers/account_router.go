package routers

import (
	"github.com/gin-gonic/gin"
	"github.com/wfu-work/nav-common-go-lib/middlewares"
)

type AccountRouter struct{}

func (r AccountRouter) InitAccountRouter(group *gin.RouterGroup) {
	routerLogger := group.Group("accounts").Use(middlewares.ApiLogger())
	router := group.Group("accounts")
	{
		routerLogger.POST("", accountApi.Create)
		routerLogger.PUT(":guid", accountApi.Update)
		routerLogger.DELETE(":guid", accountApi.DeleteByGuid)
		routerLogger.POST(":guid/enable", accountApi.Enable)
		routerLogger.POST(":guid/disable", accountApi.Disable)
		routerLogger.POST(":guid/refresh", accountApi.Refresh)
		routerLogger.POST(":guid/refresh-usage", accountApi.RefreshUsage)
		routerLogger.POST(":guid/test", accountApi.Test)
		routerLogger.POST("fetch-models", accountApi.FetchModels)
		routerLogger.POST("parse-login-callback", accountApi.ParseLoginCallback)
		routerLogger.POST("reorder", accountApi.Reorder)
	}
	{
		router.GET("list", accountApi.List)
		router.GET("list/all", accountApi.ListAll)
		router.GET(":guid", accountApi.GetByGuid)
	}
}
