package routers

import (
	"github.com/gin-gonic/gin"
	"github.com/wfu-work/nav-common-go-lib/middlewares"
)

type AccountGroupRouter struct{}

func (r AccountGroupRouter) InitAccountGroupRouter(group *gin.RouterGroup) {
	routerLogger := group.Group("account-groups").Use(middlewares.ApiLogger())
	router := group.Group("account-groups")
	{
		routerLogger.POST("", accountGroupApi.Create)
		routerLogger.PUT(":guid", accountGroupApi.Update)
		routerLogger.DELETE(":guid", accountGroupApi.Delete)
	}
	{
		router.GET("", accountGroupApi.List)
		router.GET(":guid", accountGroupApi.Detail)
	}
}
