package routers

import "github.com/gin-gonic/gin"

type HealthRouter struct{}

func (r HealthRouter) InitHealthRouter(group *gin.RouterGroup) {
	group.GET("/healthz", func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true, "name": "FreeAiGo"})
	})
}
