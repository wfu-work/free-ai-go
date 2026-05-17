package routers

import (
	"github.com/gin-gonic/gin"
	"github.com/wfu-work/nav-common-go-lib/middlewares"
)

type PlatformKeyRouter struct{}

func (r PlatformKeyRouter) InitPlatformKeyRouter(group *gin.RouterGroup) {
	routerLogger := group.Group("platform-keys").Use(middlewares.ApiLogger())
	router := group.Group("platform-keys")
	{
		routerLogger.POST("", platformKeyApi.Create)
		routerLogger.PUT(":guid", platformKeyApi.Update)
		routerLogger.DELETE(":guid", platformKeyApi.Delete)
		routerLogger.POST(":guid/enable", platformKeyApi.Enable)
		routerLogger.POST(":guid/disable", platformKeyApi.Disable)
	}
	{
		router.GET("", platformKeyApi.List)
		router.GET(":guid", platformKeyApi.Detail)
	}
}
