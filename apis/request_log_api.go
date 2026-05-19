package apis

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/cast"
	"github.com/wfu-work/nav-common-go-lib/global"
	"github.com/wfu-work/nav-common-go-lib/response"
	"go.uber.org/zap"
)

type RequestLogApi struct{}

// List 分页获取请求日志列表
// @Summary 分页获取请求日志列表
// @Description 分页获取请求日志列表
// @Tags 请求日志模块
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param data query domains.PageInfo true "页码, 每页大小"
// @Success 200 {object} response.Response{data=domains.PageResult,msg=string}
// @Router /request-logs/list [get]
func (a RequestLogApi) List(c *gin.Context) {
	params := queryParams(c)
	if err := verifyPageParams(params); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	list, total, err := requestLogService.List(params)
	if err != nil {
		global.NAV_LOG.Error("获取失败!", zap.Error(err))
		response.Fail(nil, c)
		return
	}
	response.Ok(pageResult(list, total, params), c)
}

// ListAll 获取所有请求日志列表
// @Summary 获取所有请求日志列表
// @Description 获取所有请求日志列表
// @Tags 请求日志模块
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Success 200 {object} response.Response{data=[]domains.RequestLog,msg=string}
// @Router /request-logs/list/all [get]
func (a RequestLogApi) ListAll(c *gin.Context) {
	limit := cast.ToInt(c.Query("limit"))
	since := cast.ToInt64(c.Query("since"))
	list, err := requestLogService.ListAll(limit, since)
	if err != nil {
		global.NAV_LOG.Error("获取失败!", zap.Error(err))
		response.Fail(nil, c)
		return
	}
	response.Ok(list, c)
}

// GetByGuid 获取请求日志信息
// @Summary 根据guid获取请求日志
// @Description 根据guid获取请求日志
// @Tags 请求日志模块
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param guid path string true "请求日志guid"
// @Success 200 {object} response.Response{data=domains.RequestLog,msg=string}
// @Router /request-logs/{guid} [get]
func (a RequestLogApi) GetByGuid(c *gin.Context) {
	log, err := requestLogService.GetByGuid(c.Param("guid"))
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(log, c)
}

// DeleteByGuid 删除请求日志
// @Summary 根据guid删除请求日志
// @Description 根据guid删除请求日志
// @Tags 请求日志模块
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param guid path string true "请求日志guid"
// @Success 200 {object} response.Response{data=bool,msg=string}
// @Router /request-logs/{guid} [delete]
func (a RequestLogApi) DeleteByGuid(c *gin.Context) {
	if err := requestLogService.DeleteByGuid(c.Param("guid")); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(true, c)
}

// Clear 清理请求日志
// @Summary 清理请求日志
// @Description 清理请求日志
// @Tags 请求日志模块
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param before query int false "清理该时间戳前的日志"
// @Param retentionDays query int false "保留天数"
// @Success 200 {object} response.Response{data=bool,msg=string}
// @Router /request-logs [delete]
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
