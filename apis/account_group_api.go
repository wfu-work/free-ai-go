package apis

import (
	"freeai/services"

	"github.com/gin-gonic/gin"
	"github.com/wfu-work/nav-common-go-lib/global"
	"github.com/wfu-work/nav-common-go-lib/response"
	"go.uber.org/zap"
)

type AccountGroupApi struct{}

// Create 创建账号分组
// @Summary 创建账号分组
// @Description 创建账号分组
// @Tags 账号分组模块
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param data body services.AccountGroupInput true "账号分组信息"
// @Success 200 {object} response.Response{data=domains.AccountGroup,msg=string}
// @Router /account-groups [post]
func (a AccountGroupApi) Create(c *gin.Context) {
	var input services.AccountGroupInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	group, err := accountGroupService.Create(input)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(group, c)
}

// List 分页获取账号分组列表
// @Summary 分页获取账号分组列表
// @Description 分页获取账号分组列表
// @Tags 账号分组模块
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param data query domains.PageInfo true "页码, 每页大小"
// @Success 200 {object} response.Response{data=domains.PageResult,msg=string}
// @Router /account-groups/list [get]
func (a AccountGroupApi) List(c *gin.Context) {
	params := queryParams(c)
	if err := verifyPageParams(params); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	list, total, err := accountGroupService.List(params)
	if err != nil {
		global.NAV_LOG.Error("获取失败!", zap.Error(err))
		response.Fail(nil, c)
		return
	}
	response.Ok(pageResult(list, total, params), c)
}

// ListAll 获取所有账号分组列表
// @Summary 获取所有账号分组列表
// @Description 获取所有账号分组列表
// @Tags 账号分组模块
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Success 200 {object} response.Response{data=[]domains.AccountGroup,msg=string}
// @Router /account-groups/list/all [get]
func (a AccountGroupApi) ListAll(c *gin.Context) {
	list, err := accountGroupService.ListAll()
	if err != nil {
		global.NAV_LOG.Error("获取失败!", zap.Error(err))
		response.Fail(nil, c)
		return
	}
	response.Ok(list, c)
}

// GetByGuid 获取账号分组信息
// @Summary 根据guid获取账号分组
// @Description 根据guid获取账号分组
// @Tags 账号分组模块
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param guid path string true "账号分组guid"
// @Success 200 {object} response.Response{data=domains.AccountGroup,msg=string}
// @Router /account-groups/{guid} [get]
func (a AccountGroupApi) GetByGuid(c *gin.Context) {
	group, err := accountGroupService.GetByGuid(c.Param("guid"))
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(group, c)
}

// Update 更新账号分组
// @Router /account-groups/{guid} [put]
func (a AccountGroupApi) Update(c *gin.Context) {
	var input services.AccountGroupInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	group, err := accountGroupService.Update(c.Param("guid"), input)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(group, c)
}

// DeleteByGuid 删除账号分组
// @Summary 根据guid删除账号分组
// @Description 根据guid删除账号分组
// @Tags 账号分组模块
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param guid path string true "账号分组guid"
// @Success 200 {object} response.Response{data=bool,msg=string}
// @Router /account-groups/{guid} [delete]
func (a AccountGroupApi) DeleteByGuid(c *gin.Context) {
	if err := accountGroupService.DeleteByGuid(c.Param("guid")); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(true, c)
}
