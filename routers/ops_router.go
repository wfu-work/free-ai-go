package routers

import "github.com/gin-gonic/gin"

type OpsRouter struct{}

func (r OpsRouter) InitOpsRouter(group *gin.RouterGroup) {
	group.GET("ops/metrics", opsApi.Metrics)
	group.GET("ops/gateway-config", opsApi.GatewayConfig)
	group.PUT("ops/gateway-config", opsApi.SaveGatewayConfig)
	group.GET("ops/stats", opsApi.Stats)
	group.GET("ops/routes", opsApi.Routes)
	group.GET("ops/account-health", opsApi.AccountHealth)
	group.GET("ops/master-key", opsApi.MasterKey)
}
