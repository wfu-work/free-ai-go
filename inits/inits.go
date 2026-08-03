package inits

import (
	_ "embed"
	"fmt"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/robfig/cron/v3"
	"github.com/wfu-work/free-ai-go/domains"
	"github.com/wfu-work/free-ai-go/routers"
	"github.com/wfu-work/free-ai-go/scheduleds"
	"github.com/wfu-work/free-ai-go/services"
	"github.com/wfu-work/free-ai-go/utils"
	"github.com/wfu-work/free-ai-go/webs"
	commoninits "github.com/wfu-work/nav-common-go-lib/inits"
	commonscheduleds "github.com/wfu-work/nav-common-go-lib/scheduleds"
)

//go:embed config.yaml
var defaultConfig []byte

func Init() {
	if err := utils.NewDefaultConfigManager(defaultConfig).Ensure(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "prepare config failed: %v\n", err)
		os.Exit(1)
	}
	sysInit := commoninits.SysInit{}
	sysInit.OnWebInit(initWebRoutes)
	sysInit.OnTableInit(func() {
		domains.RegisterTables()
	})
	sysInit.OnRouterInit(func(publicGroup *gin.RouterGroup, privateGroup *gin.RouterGroup) {
		routers.RouterGroupApp.InitGatewayRouters(publicGroup, privateGroup)
	})
	sysInit.OnOtherInit(func() {
		scheduleds.Bootstrap()
	})
	sysInit.OnScheInit(func(timers commonscheduleds.Timer, options []cron.Option) {
		scheduleds.Register(timers, options)
	})
	sysInit.OnClearInit(func() []commonscheduleds.ClearDB {
		return []commonscheduleds.ClearDB{}
	})
	sysInit.Init()
}

// initWebRoutes must register all routes through one callback. SysInit keeps
// only the last OnWebInit callback, so splitting these registrations would
// leave the public proxy routes unregistered and send /v1 requests to SPA fallback.
func initWebRoutes(router *gin.Engine) {
	routers.RouterGroupApp.InitProxyWebRouter(router)
	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true, "name": "FreeAiGo"})
	})
	router.GET("/readyz", func(c *gin.Context) {
		status := services.Readiness(c.Request.Context())
		code := 200
		if !status.Ready {
			code = 503
		}
		c.JSON(code, status)
	})
	_ = webs.InitStatic(router, "/api", services.Config().ProxyPrefix, "/healthz", "/readyz")
}
