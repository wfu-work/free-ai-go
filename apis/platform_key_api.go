package apis

import (
	"freeai/services"

	"github.com/gin-gonic/gin"
	"github.com/wfu-work/nav-common-go-lib/domains"
	"github.com/wfu-work/nav-common-go-lib/global"
	"github.com/wfu-work/nav-common-go-lib/response"
	"github.com/wfu-work/nav-common-go-lib/utils"
	"go.uber.org/zap"
)

type PlatformKeyApi struct{}

// Create 创建密钥
// @Summary 创建密钥
// @Description 创建平台密钥，明文只返回一次
// @Tags 密钥模块
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param data body services.CreatePlatformKeyInput true "密钥信息"
// @Success 200 {object} response.Response{data=services.CreatePlatformKeyOutput,msg=string}
// @Router /platform-keys [post]
func (a PlatformKeyApi) Create(c *gin.Context) {
	var input services.CreatePlatformKeyInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	out, err := platformKeyService.Create(input)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(out, c)
}

// List 分页获取密钥列表
// @Summary 分页获取密钥列表
// @Description 分页获取密钥列表
// @Tags 密钥模块
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param data  query domains.PageInfo true  "页码, 每页大小"
// @Success 200  {object}  response.Response{data=domains.PageResult,msg=string}  "分页获取设备列表,返回包括列表,总数,页码,每页数量"
// @Router /platform-keys/list [get]
func (a PlatformKeyApi) List(c *gin.Context) {
	query := c.Request.URL.Query()
	params := make(map[string]string)
	for key, val := range query {
		if len(val) > 0 {
			params[key] = val[0]
		}
	}
	err := utils.Verify(utils.ToPageInfo(params), utils.PageInfoVerify)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	list, total, err := platformKeyService.List(params)
	if err != nil {
		global.NAV_LOG.Error("获取失败!", zap.Error(err))
		response.Fail(nil, c)
		return
	}
	response.Ok(domains.PageResult{
		Data:  list,
		Total: total,
		Page:  utils.Str2Int(params["page"]),
		Size:  utils.Str2Int(params["size"]),
	}, c)
}

// ListAll 获取所有密钥列表
// @Summary 获取所有密钥列表
// @Description 获取所有密钥列表
// @Tags 密钥模块
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Success 200 {object} response.Response{data=[]domains.PlatformKey,msg=string}
// @Router /platform-keys/list/all [get]
func (a PlatformKeyApi) ListAll(c *gin.Context) {
	list, err := platformKeyService.ListAll()
	if err != nil {
		global.NAV_LOG.Error("获取失败!", zap.Error(err))
		response.Fail(nil, c)
		return
	}
	response.Ok(list, c)
}

// Stats 获取平台密钥统计
// @Router /platform-keys/stats [get]
func (a PlatformKeyApi) Stats(c *gin.Context) {
	stats, err := platformKeyService.Stats()
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(stats, c)
}

// GetByGuid 获取密钥信息
// @Summary 根据guid获取密钥
// @Description 根据guid获取密钥
// @Tags 密钥模块
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param guid path string true "密钥guid"
// @Success 200 {object} response.Response{data=domains.PlatformKey,msg=string}
// @Router /platform-keys/{guid} [get]
func (a PlatformKeyApi) GetByGuid(c *gin.Context) {
	key, err := platformKeyService.GetByGuid(c.Param("guid"))
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(key, c)
}

// Update 更新密钥
// @Summary 更新密钥
// @Description 根据guid更新密钥
// @Tags 密钥模块
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param guid path string true "密钥guid"
// @Param data body services.CreatePlatformKeyInput true "密钥信息"
// @Success 200 {object} response.Response{data=domains.PlatformKey,msg=string}
// @Router /platform-keys/{guid} [put]
func (a PlatformKeyApi) Update(c *gin.Context) {
	var input services.CreatePlatformKeyInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	key, err := platformKeyService.Update(c.Param("guid"), input)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(key, c)
}

// DeleteByGuid 删除密钥
// @Summary 根据guid删除密钥
// @Description 根据guid删除密钥
// @Tags 密钥模块
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param guid path string true "密钥guid"
// @Success 200 {object} response.Response{data=bool,msg=string}
// @Router /platform-keys/{guid} [delete]
func (a PlatformKeyApi) DeleteByGuid(c *gin.Context) {
	if err := platformKeyService.DeleteByGuid(c.Param("guid")); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(true, c)
}

// Enable 启用密钥
// @Router /platform-keys/{guid}/enable [post]
func (a PlatformKeyApi) Enable(c *gin.Context) {
	if err := platformKeyService.SetEnabled(c.Param("guid"), true); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(true, c)
}

// Disable 禁用密钥
// @Router /platform-keys/{guid}/disable [post]
func (a PlatformKeyApi) Disable(c *gin.Context) {
	if err := platformKeyService.SetEnabled(c.Param("guid"), false); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(true, c)
}
