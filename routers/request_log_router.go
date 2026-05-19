package routers

import (
	"github.com/gin-gonic/gin"
	"github.com/wfu-work/nav-common-go-lib/middlewares"
)

type RequestLogRouter struct{}

func (r RequestLogRouter) InitRequestLogRouter(group *gin.RouterGroup) {
	routerLogger := group.Group("request-logs").Use(middlewares.ApiLogger())
	router := group.Group("request-logs")
	{
		router.GET("list", requestLogApi.List)
		router.GET("list/all", requestLogApi.ListAll)
		router.GET(":guid", requestLogApi.GetByGuid)
		routerLogger.DELETE(":guid", requestLogApi.DeleteByGuid)
		routerLogger.DELETE("", requestLogApi.Clear)
	}
}
