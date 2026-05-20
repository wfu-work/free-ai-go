package inits

import (
	"os"

	"freeai/domains"
	"freeai/routers"
	fmgscheduleds "freeai/scheduleds"
	"freeai/services"

	"github.com/gin-gonic/gin"
	"github.com/robfig/cron/v3"
	"github.com/wfu-work/nav-common-go-lib/global"
	commoninits "github.com/wfu-work/nav-common-go-lib/inits"
	commonscheduleds "github.com/wfu-work/nav-common-go-lib/scheduleds"
	"go.uber.org/zap"
)

func Init() {
	sysInit := commoninits.SysInit{}
	sysInit.OnWebInit(func(router *gin.Engine) {
		routers.RouterGroupApp.InitProxyWebRouter(router)
		router.GET("/healthz", func(c *gin.Context) {
			c.JSON(200, gin.H{"ok": true, "name": "FreeAiGo"})
		})
	})
	sysInit.OnTableInit(func() {
		registerTables()
	})
	sysInit.OnRouterInit(func(publicGroup *gin.RouterGroup, privateGroup *gin.RouterGroup) {
		routers.RouterGroupApp.InitFreeModelRouters(publicGroup, privateGroup)
	})
	sysInit.OnOtherInit(func() {
		services.StartOpenAIOAuthCallbackServer()
		fmgscheduleds.Bootstrap()
	})
	sysInit.OnScheInit(func(timers commonscheduleds.Timer, options []cron.Option) {
		fmgscheduleds.Register(timers, options)
	})
	sysInit.OnClearInit(func() []commonscheduleds.ClearDB {
		return []commonscheduleds.ClearDB{}
	})
	sysInit.Init()
}

func registerTables() {
	db := global.NAV_DB
	if err := db.AutoMigrate(
		domains.Account{},
		domains.AccountGroup{},
		domains.AccountQuota{},
		domains.ModelMapping{},
		domains.PlatformKey{},
		domains.RequestLog{},
		domains.RouteState{},
		domains.AuditLog{},
		domains.SystemConfig{},
	); err != nil {
		global.NAV_LOG.Error("register FreeAiGo tables failed", zap.Error(err))
		os.Exit(1)
	}
	global.NAV_LOG.Info("register FreeAiGo tables success")
}
