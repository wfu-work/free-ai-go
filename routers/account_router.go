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
		routerLogger.PUT(":guid", accountApi.Update)
		routerLogger.DELETE(":guid", accountApi.DeleteByGuid)
		routerLogger.POST(":guid/enable", accountApi.Enable)
		routerLogger.POST(":guid/disable", accountApi.Disable)
		routerLogger.POST(":guid/refresh-usage", accountApi.RefreshUsage)
		routerLogger.POST(":guid/reset-credits/consume", accountApi.ConsumeResetCredit)
		routerLogger.POST(":guid/probe", accountApi.Probe)
		routerLogger.POST("fetch-models", accountApi.FetchModels)
		routerLogger.POST("reorder", accountApi.Reorder)
	}
	{
		// OAuth 文件、手动 Token、API Key 和授权码都属于敏感凭据，不经过可能记录请求体的 API 日志中间件。
		router.POST("", accountApi.AddManual)
		router.POST("api-key", accountApi.AddAPIKey)
		router.POST("import", accountApi.Import)
		router.POST("import-file", accountApi.ImportFile)
		router.POST("oauth/sessions", accountApi.StartOAuth)
		router.GET("oauth/sessions/:id", accountApi.GetOAuth)
		router.POST("oauth/sessions/:id/complete", accountApi.CompleteOAuth)
		router.DELETE("oauth/sessions/:id", accountApi.CancelOAuth)
		router.GET("list", accountApi.List)
		router.GET("list/all", accountApi.ListAll)
		router.GET(":guid", accountApi.GetByGuid)
		router.GET(":guid/reset-credits", accountApi.ResetCredits)
		router.GET(":guid/export", accountApi.Export)
	}
}
