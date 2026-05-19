package apis

import (
	"freeai/services"

	"github.com/gin-gonic/gin"
	"github.com/wfu-work/nav-common-go-lib/global"
	"github.com/wfu-work/nav-common-go-lib/response"
	"go.uber.org/zap"
)

type QuotaApi struct{}

// Upsert 写入或更新账号额度
// @Summary 写入或更新账号额度
// @Description 写入或更新账号额度
// @Tags 额度模块
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param guid path string true "账号guid"
// @Param data body services.QuotaInput true "额度信息"
// @Success 200 {object} response.Response{data=domains.AccountQuota,msg=string}
// @Router /accounts/{guid}/quotas [post]
func (a QuotaApi) Upsert(c *gin.Context) {
	var input services.QuotaInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	if input.AccountGuid == "" {
		input.AccountGuid = c.Param("guid")
	}
	quota, err := quotaService.Upsert(input)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(quota, c)
}

// List 分页获取额度列表
// @Summary 分页获取额度列表
// @Description 分页获取额度列表
// @Tags 额度模块
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param data query domains.PageInfo true "页码, 每页大小"
// @Success 200 {object} response.Response{data=domains.PageResult,msg=string}
// @Router /quotas/list [get]
func (a QuotaApi) List(c *gin.Context) {
	params := queryParams(c)
	if err := verifyPageParams(params); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	list, total, err := quotaService.List(params)
	if err != nil {
		global.NAV_LOG.Error("获取失败!", zap.Error(err))
		response.Fail(nil, c)
		return
	}
	response.Ok(pageResult(list, total, params), c)
}

// ListAll 获取所有额度列表
// @Summary 获取所有额度列表
// @Description 获取所有额度列表
// @Tags 额度模块
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Success 200 {object} response.Response{data=[]domains.AccountQuota,msg=string}
// @Router /quotas/list/all [get]
func (a QuotaApi) ListAll(c *gin.Context) {
	list, err := quotaService.ListAll(c.Query("accountGuid"))
	if err != nil {
		global.NAV_LOG.Error("获取失败!", zap.Error(err))
		response.Fail(nil, c)
		return
	}
	response.Ok(list, c)
}

// ListByAccount 获取账号额度列表
// @Summary 获取账号额度列表
// @Description 获取账号额度列表
// @Tags 额度模块
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param guid path string true "账号guid"
// @Success 200 {object} response.Response{data=[]domains.AccountQuota,msg=string}
// @Router /accounts/{guid}/quotas [get]
func (a QuotaApi) ListByAccount(c *gin.Context) {
	list, err := quotaService.ListByAccount(c.Param("guid"))
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(list, c)
}
