package apis

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/cast"
	"github.com/wfu-work/nav-common-go-lib/response"
)

type RequestLogApi struct{}

func (a RequestLogApi) List(c *gin.Context) {
	list, err := requestLogService.List(cast.ToInt(c.DefaultQuery("limit", "200")))
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(list, c)
}

func (a RequestLogApi) Detail(c *gin.Context) {
	log, err := requestLogService.Get(c.Param("guid"))
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(log, c)
}

func (a RequestLogApi) Clear(c *gin.Context) {
	before := cast.ToInt64(c.Query("before"))
	if before == 0 {
		days := cast.ToInt(c.Query("retentionDays"))
		if days > 0 {
			before = time.Now().Add(-time.Duration(days) * 24 * time.Hour).UnixMilli()
		}
	}
	if err := requestLogService.ClearBefore(before); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(true, c)
}
