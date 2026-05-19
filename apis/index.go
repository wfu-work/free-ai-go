package apis

import (
	"freeai/services"

	"github.com/gin-gonic/gin"
	commonDomains "github.com/wfu-work/nav-common-go-lib/domains"
	"github.com/wfu-work/nav-common-go-lib/utils"
)

var ApiGroupApp = new(ApiGroup)

type ApiGroup struct {
	AccountApi
	AccountGroupApi
	PlatformKeyApi
	ModelApi
	QuotaApi
	RequestLogApi
	OpsApi
	ProxyApi
}

var (
	accountService      = services.AccountServiceApp
	accountGroupService = services.AccountGroupServiceApp
	platformKeyService  = services.PlatformKeyServiceApp
	modelService        = services.ModelServiceApp
	quotaService        = services.QuotaServiceApp
	requestLogService   = services.RequestLogServiceApp
	proxyService        = services.ProxyServiceApp
)

func queryParams(c *gin.Context) map[string]string {
	query := c.Request.URL.Query()
	params := make(map[string]string)
	for key, val := range query {
		if len(val) > 0 {
			params[key] = val[0]
		}
	}
	return params
}

func verifyPageParams(params map[string]string) error {
	return utils.Verify(utils.ToPageInfo(params), utils.PageInfoVerify)
}

func pageResult(data any, total int64, params map[string]string) commonDomains.PageResult {
	return commonDomains.PageResult{
		Data:  data,
		Total: total,
		Page:  utils.Str2Int(params["page"]),
		Size:  utils.Str2Int(params["size"]),
	}
}
