package apis

import (
	"github.com/gin-gonic/gin"
	"github.com/wfu-work/free-ai-go/services"
	"github.com/wfu-work/nav-common-go-lib/global"
	"github.com/wfu-work/nav-common-go-lib/response"
	"go.uber.org/zap"
)

type ModelApi struct{}

// Sync 从官方账号同步模型目录。
// @Router /models/sync [post]
func (a ModelApi) Sync(c *gin.Context) {
	var input services.ModelSyncInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	result, err := accountService.SyncModels(input)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(result, c)
}

// SyncPricing 从 OpenAI 官方定价文档同步 API 参考价。
// @Router /models/pricing/sync [post]
func (a ModelApi) SyncPricing(c *gin.Context) {
	result, err := modelPricingService.SyncOfficial(c.Request.Context())
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(result, c)
}

// List 分页获取模型目录
// @Summary 分页获取模型目录
// @Description 分页获取模型目录
// @Tags 模型模块
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param data query domains.PageInfo true "页码, 每页大小"
// @Success 200 {object} response.Response{data=domains.PageResult,msg=string}
// @Router /models/list [get]
func (a ModelApi) List(c *gin.Context) {
	params := queryParams(c)
	if err := verifyPageParams(params); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	list, total, err := modelService.List(params)
	if err != nil {
		global.NAV_LOG.Error("获取失败!", zap.Error(err))
		response.Fail(nil, c)
		return
	}
	response.Ok(pageResult(list, total, params), c)
}

// ListAll 获取全部模型目录
// @Summary 获取全部模型目录
// @Description 获取全部模型目录
// @Tags 模型模块
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Router /models/list/all [get]
func (a ModelApi) ListAll(c *gin.Context) {
	list, err := modelService.ListAll()
	if err != nil {
		global.NAV_LOG.Error("获取失败!", zap.Error(err))
		response.Fail(nil, c)
		return
	}
	response.Ok(list, c)
}

// GetByGuid 获取模型目录信息
// @Summary 根据guid获取模型目录
// @Description 根据guid获取模型目录
// @Tags 模型模块
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param guid path string true "模型目录guid"
// @Router /models/{guid} [get]
func (a ModelApi) GetByGuid(c *gin.Context) {
	model, err := modelService.GetByGuid(c.Param("guid"))
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(model, c)
}

// Update 更新模型对外策略
// @Router /models/{guid} [put]
func (a ModelApi) Update(c *gin.Context) {
	var input services.ModelPolicyInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	model, err := modelService.Update(c.Param("guid"), input)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(model, c)
}

// Enable 启用模型对外路由
// @Router /models/{guid}/enable [post]
func (a ModelApi) Enable(c *gin.Context) {
	if err := modelService.SetEnabled(c.Param("guid"), true); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(true, c)
}

// Disable 禁用模型对外路由
// @Router /models/{guid}/disable [post]
func (a ModelApi) Disable(c *gin.Context) {
	if err := modelService.SetEnabled(c.Param("guid"), false); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(true, c)
}

// DeleteByGuid 删除没有可用账号的模型目录。
// @Router /models/{guid} [delete]
func (a ModelApi) DeleteByGuid(c *gin.Context) {
	if err := modelService.DeleteByGuid(c.Param("guid")); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(true, c)
}

// Accounts 获取可使用指定模型的账号和最近同步状态。
// @Router /models/{guid}/accounts [get]
func (a ModelApi) Accounts(c *gin.Context) {
	items, err := modelService.Accounts(c.Param("guid"))
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(items, c)
}
