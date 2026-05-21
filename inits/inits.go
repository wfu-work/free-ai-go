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
	"github.com/wfu-work/nav-common-go-lib/global"
	commoninits "github.com/wfu-work/nav-common-go-lib/inits"
	commonscheduleds "github.com/wfu-work/nav-common-go-lib/scheduleds"
	"go.uber.org/zap"
)

//go:embed config.yaml
var defaultConfig []byte

func Init() {
	if err := utils.NewDefaultConfigManager(defaultConfig).Ensure(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "prepare config failed: %v\n", err)
		os.Exit(1)
	}
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
		scheduleds.Bootstrap()
	})
	sysInit.OnScheInit(func(timers commonscheduleds.Timer, options []cron.Option) {
		scheduleds.Register(timers, options)
	})
	sysInit.OnClearInit(func() []commonscheduleds.ClearDB {
		return []commonscheduleds.ClearDB{}
	})
	sysInit.OnWebInit(func(router *gin.Engine) {
		_ = webs.InitStatic(router)
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
