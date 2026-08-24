package apis

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/wfu-work/free-ai-go/services"
	"github.com/wfu-work/nav-common-go-lib/global"
	"github.com/wfu-work/nav-common-go-lib/response"
	"go.uber.org/zap"
)

type AccountApi struct{}

const maxAccountImportRequestBytes = (16 << 20) + (64 << 10)

// Import 导入 Codex OAuth 账号文件。
func (a AccountApi) Import(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxAccountImportRequestBytes)
	var input services.ImportAccountInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	account, err := accountService.Import(input)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(account, c)
	accountService.SyncOfficialAccountAsync(account.Guid)
}

// ImportFile 自动识别并导入 FreeAI 原生或 sub2api-data v1 账号文件。
func (a AccountApi) ImportFile(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxAccountImportRequestBytes)
	var input services.ImportAccountInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	result, err := accountService.ImportFile(input)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(result, c)
}

// AddManual 使用手动填写的 OAuth Token 添加官方账号。
func (a AccountApi) AddManual(c *gin.Context) {
	var input services.ManualAccountInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	account, err := accountService.AddManual(c.Request.Context(), input)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(account, c)
	accountService.SyncOfficialAccountAsync(account.Guid)
}

// AddAPIKey 添加独立 OpenAI Platform 图片 API 账号。
// API Key 请求体不经过日志中间件，响应不会返回明文凭据。
func (a AccountApi) AddAPIKey(c *gin.Context) {
	var input services.APIKeyAccountInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	account, err := accountService.AddAPIKey(c.Request.Context(), input)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(account, c)
}

// StartOAuth 创建浏览器 PKCE 或设备码官方授权会话。
func (a AccountApi) StartOAuth(c *gin.Context) {
	var input services.AccountOAuthStartInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	result, err := accountOAuthService.Start(c.Request.Context(), input)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(result, c)
}

// GetOAuth 返回官方授权会话的最新状态。
func (a AccountApi) GetOAuth(c *gin.Context) {
	result, err := accountOAuthService.Get(c.Param("id"))
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(result, c)
}

// CompleteOAuth 手动提交浏览器 OAuth 回调 URL。
func (a AccountApi) CompleteOAuth(c *gin.Context) {
	var input services.AccountOAuthCompleteInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	result, err := accountOAuthService.CompleteBrowser(c.Param("id"), input)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(result, c)
}

// CancelOAuth 取消官方授权会话。
func (a AccountApi) CancelOAuth(c *gin.Context) {
	result, err := accountOAuthService.Cancel(c.Param("id"))
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(result, c)
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
	var input services.UpdateAccountInput
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

// Export 导出当前账号的规范 OAuth 文件。该响应包含敏感令牌。
func (a AccountApi) Export(c *gin.Context) {
	data, account, err := accountService.Export(c.Param("guid"))
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	fileID := strings.NewReplacer("/", "_", "\\", "_", "\"", "_").Replace(account.ChatGPTAccountID)
	if fileID == "" {
		fileID = account.Guid
	}
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s.json\"", fileID))
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "application/json; charset=utf-8", data)
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

// ResetCredits 查询官方账号的额度重置券。
func (a AccountApi) ResetCredits(c *gin.Context) {
	result, err := services.AccountResetCreditServiceApp.List(c.Request.Context(), c.Param("guid"))
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(result, c)
}

// ConsumeResetCredit 幂等消耗一张官方额度重置券，并重新同步账号额度。
func (a AccountApi) ConsumeResetCredit(c *gin.Context) {
	var input services.ConsumeAccountResetCreditInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	result, err := services.AccountResetCreditServiceApp.Consume(c.Request.Context(), c.Param("guid"), input)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(result, c)
}

// Probe 主动发起一个极小的 Codex Responses 请求并采样额度响应头。
func (a AccountApi) Probe(c *gin.Context) {
	var input services.AccountTestInput
	_ = c.ShouldBindJSON(&input)
	result, err := accountService.Probe(c.Param("guid"), input)
	if err != nil {
		response.FailWithMessage(err.Error(), c)
		return
	}
	response.Ok(result, c)
}

// FetchModels 同步账号的官方模型目录
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
