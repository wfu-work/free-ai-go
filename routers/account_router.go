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
		routerLogger.DELETE(":guid", accountApi.Delete)
		routerLogger.POST(":guid/enable", accountApi.Enable)
		routerLogger.POST(":guid/disable", accountApi.Disable)
		routerLogger.POST(":guid/refresh", accountApi.Refresh)
		routerLogger.POST(":guid/test", accountApi.Test)
		routerLogger.POST("reorder", accountApi.Reorder)
	}
	{
		router.GET("", accountApi.List)
		router.GET(":guid", accountApi.Detail)
	}
}
