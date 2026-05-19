package apis

import (
	"freeai/services"

	"github.com/gin-gonic/gin"
	"github.com/wfu-work/nav-common-go-lib/global"
	"github.com/wfu-work/nav-common-go-lib/response"
	"go.uber.org/zap"
)

type ModelApi struct{}

// Create 创建模型映射
// @Summary 创建模型映射
// @Description 创建模型映射
// @Tags 模型模块
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param data body services.ModelInput true "模型映射信息"
// @Success 200 {object} response.Response{data=domains.ModelMapping,msg=string}
// @Router /models [post]
func (a ModelApi) Create(c *gin.Context) {
	var input services.ModelInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	model, err := modelService.Create(input)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(model, c)
}

// List 分页获取模型映射列表
// @Summary 分页获取模型映射列表
// @Description 分页获取模型映射列表
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

// ListAll 获取所有模型映射列表
// @Summary 获取所有模型映射列表
// @Description 获取所有模型映射列表
// @Tags 模型模块
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Success 200 {object} response.Response{data=[]domains.ModelMapping,msg=string}
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

// GetByGuid 获取模型映射信息
// @Summary 根据guid获取模型映射
// @Description 根据guid获取模型映射
// @Tags 模型模块
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param guid path string true "模型映射guid"
// @Success 200 {object} response.Response{data=domains.ModelMapping,msg=string}
// @Router /models/{guid} [get]
func (a ModelApi) GetByGuid(c *gin.Context) {
	model, err := modelService.GetByGuid(c.Param("guid"))
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(model, c)
}

// Update 更新模型映射
// @Router /models/{guid} [put]
func (a ModelApi) Update(c *gin.Context) {
	var input services.ModelInput
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

// DeleteByGuid 删除模型映射
// @Summary 根据guid删除模型映射
// @Description 根据guid删除模型映射
// @Tags 模型模块
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param guid path string true "模型映射guid"
// @Success 200 {object} response.Response{data=bool,msg=string}
// @Router /models/{guid} [delete]
func (a ModelApi) DeleteByGuid(c *gin.Context) {
	if err := modelService.DeleteByGuid(c.Param("guid")); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(true, c)
}

// Enable 启用模型映射
// @Router /models/{guid}/enable [post]
func (a ModelApi) Enable(c *gin.Context) {
	if err := modelService.SetEnabled(c.Param("guid"), true); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(true, c)
}

// Disable 禁用模型映射
// @Router /models/{guid}/disable [post]
func (a ModelApi) Disable(c *gin.Context) {
	if err := modelService.SetEnabled(c.Param("guid"), false); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(true, c)
}
