package apis

import (
	"freeai/services"

	"github.com/gin-gonic/gin"
	"github.com/wfu-work/nav-common-go-lib/global"
	"github.com/wfu-work/nav-common-go-lib/response"
	"go.uber.org/zap"
)

type AccountApi struct{}

// Create 创建账号
// @Summary 创建账号
// @Description 创建账号
// @Tags 账号模块
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param data body services.CreateAccountInput true "账号信息"
// @Success 200 {object} response.Response{data=domains.Account,msg=string}
// @Router /accounts [post]
func (a AccountApi) Create(c *gin.Context) {
	var input services.CreateAccountInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	account, err := accountService.Create(input)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(account, c)
}

// Update 更新账号
// @Summary 更新账号
// @Description 根据guid更新账号
// @Tags 账号模块
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param guid path string true "账号guid"
// @Param data body services.CreateAccountInput true "账号信息"
// @Success 200 {object} response.Response{data=domains.Account,msg=string}
// @Router /accounts/{guid} [put]
func (a AccountApi) Update(c *gin.Context) {
	var input services.CreateAccountInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	account, err := accountService.Update(c.Param("guid"), input)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(account, c)
}

// List 分页获取账号列表
// @Summary 分页获取账号列表
// @Description 分页获取账号列表
// @Tags 账号模块
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param data query domains.PageInfo true "页码, 每页大小"
// @Success 200 {object} response.Response{data=domains.PageResult,msg=string}
// @Router /accounts/list [get]
func (a AccountApi) List(c *gin.Context) {
	params := queryParams(c)
	if err := verifyPageParams(params); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	list, total, err := accountService.List(params)
	if err != nil {
		global.NAV_LOG.Error("获取失败!", zap.Error(err))
		response.Fail(nil, c)
		return
	}
	response.Ok(pageResult(list, total, params), c)
}

// ListAll 获取所有账号列表
// @Summary 获取所有账号列表
// @Description 获取所有账号列表
// @Tags 账号模块
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Success 200 {object} response.Response{data=[]domains.Account,msg=string}
// @Router /accounts/list/all [get]
func (a AccountApi) ListAll(c *gin.Context) {
	list, err := accountService.ListAll()
	if err != nil {
		global.NAV_LOG.Error("获取失败!", zap.Error(err))
		response.Fail(nil, c)
		return
	}
	response.Ok(list, c)
}

// GetByGuid 获取账号信息
// @Summary 根据guid获取账号
// @Description 根据guid获取账号
// @Tags 账号模块
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param guid path string true "账号guid"
// @Success 200 {object} response.Response{data=domains.Account,msg=string}
// @Router /accounts/{guid} [get]
func (a AccountApi) GetByGuid(c *gin.Context) {
	account, err := accountService.GetByGuid(c.Param("guid"))
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(account, c)
}

// DeleteByGuid 删除账号
// @Summary 根据guid删除账号
// @Description 根据guid删除账号
// @Tags 账号模块
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param guid path string true "账号guid"
// @Success 200 {object} response.Response{data=bool,msg=string}
// @Router /accounts/{guid} [delete]
func (a AccountApi) DeleteByGuid(c *gin.Context) {
	if err := accountService.DeleteByGuid(c.Param("guid")); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(true, c)
}

// Enable 启用账号
// @Router /accounts/{guid}/enable [post]
func (a AccountApi) Enable(c *gin.Context) {
	if err := accountService.SetEnabled(c.Param("guid"), true); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(true, c)
}

// Disable 禁用账号
// @Router /accounts/{guid}/disable [post]
func (a AccountApi) Disable(c *gin.Context) {
	if err := accountService.SetEnabled(c.Param("guid"), false); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(true, c)
}

// Refresh 刷新账号状态
// @Router /accounts/{guid}/refresh [post]
func (a AccountApi) Refresh(c *gin.Context) {
	account, err := accountService.Refresh(c.Param("guid"))
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(account, c)
}

// RefreshUsage 刷新账号额度
// @Router /accounts/{guid}/refresh-usage [post]
func (a AccountApi) RefreshUsage(c *gin.Context) {
	result, err := accountService.RefreshUsage(c.Param("guid"))
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(result, c)
}

// Test 测试账号
// @Router /accounts/{guid}/test [post]
func (a AccountApi) Test(c *gin.Context) {
	var input services.AccountTestInput
	_ = c.ShouldBindJSON(&input)
	result, err := accountService.Test(c.Param("guid"), input)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(result, c)
}

// FetchModels 获取上游模型列表
// @Router /accounts/fetch-models [post]
func (a AccountApi) FetchModels(c *gin.Context) {
	var input services.FetchAccountModelsInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	models, err := accountService.FetchModels(input)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(gin.H{"models": models}, c)
}

// Reorder 账号排序
// @Router /accounts/reorder [post]
func (a AccountApi) Reorder(c *gin.Context) {
	var input services.ReorderAccountInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	if err := accountService.Reorder(input); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(true, c)
}
