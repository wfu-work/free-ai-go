package routers

import (
	"github.com/gin-gonic/gin"
	"github.com/wfu-work/nav-common-go-lib/middlewares"
)

type ModelRouter struct{}

func (r ModelRouter) InitModelRouter(group *gin.RouterGroup) {
	routerLogger := group.Group("models").Use(middlewares.ApiLogger())
	router := group.Group("models")
	{
		routerLogger.POST("", modelApi.Create)
		routerLogger.PUT(":guid", modelApi.Update)
		routerLogger.DELETE(":guid", modelApi.DeleteByGuid)
		routerLogger.POST(":guid/enable", modelApi.Enable)
		routerLogger.POST(":guid/disable", modelApi.Disable)
	}
	{
		router.GET("list", modelApi.List)
		router.GET("list/all", modelApi.ListAll)
		router.GET(":guid", modelApi.GetByGuid)
	}
}
