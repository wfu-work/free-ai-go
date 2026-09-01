package domains

import "strings"

const (
	VendorOpenAI              = "openai"
	VendorGoogle              = "google"
	VendorAnthropic           = "anthropic"
	ProductCodex              = "codex"
	ProductOpenAIImages       = "openai_images"
	ProductGemini             = "gemini"
	ProductClaudeCode         = "claude_code"
	CredentialOAuth           = "oauth"
	CredentialAPIKey          = "api_key"
	ProtocolOpenAIResponses   = "openai_responses"
	ProtocolOpenAIImages      = "openai_images"
	ProtocolAnthropicMessages = "anthropic_messages"

	AccountStatusAvailable = "available"
	AccountStatusLimited   = "limited"
	AccountStatusCooldown  = "cooldown"
	AccountStatusExhausted = "exhausted"
	AccountStatusDisabled  = "disabled"
	AccountStatusExpired   = "expired"
	AccountStatusInvalid   = "invalid"
	AccountStatusUnknown   = "unknown"

	QuotaStatusAvailable = "available"
	QuotaStatusLimited   = "limited"
	QuotaStatusExhausted = "exhausted"
	QuotaStatusUnknown   = "unknown"

	ResetCreditRedemptionPending   = "pending"
	ResetCreditRedemptionCompleted = "completed"

	TokenStatusActive        = "active"
	TokenStatusRefreshNeeded = "refresh_needed"
	TokenStatusRefreshFailed = "refresh_failed"
	TokenStatusInvalid       = "invalid"

	ErrorAuthFailed         = "auth_failed"
	ErrorRateLimited        = "rate_limited"
	ErrorQuotaExhausted     = "quota_exhausted"
	ErrorUpstreamTimeout    = "upstream_timeout"
	ErrorUpstreamHTTP4xx    = "upstream_http_4xx"
	ErrorUpstreamHTTP5xx    = "upstream_http_5xx"
	ErrorUpstreamFailed     = "upstream_failed"
	ErrorStreamIncomplete   = "stream_incomplete"
	ErrorClientDisconnected = "client_disconnected"
	ErrorProtocol           = "protocol_error"
	ErrorNetwork            = "network_error"
	ErrorInternal           = "internal_error"
	// ErrorUpstream5xx 保留用于兼容升级前的历史调用记录。
	ErrorUpstream5xx        = "upstream_5xx"
	ErrorModelNotSupported  = "model_not_supported"
	ErrorNoAvailableAccount = "no_available_account"
	ErrorPlatformKeyInvalid = "platform_key_invalid"
	ErrorPlatformKeyLimited = "platform_key_limited"

	DiagnosticClientClosedAfterCompletion = "client_closed_after_completion"
	DiagnosticContextCompacted            = "context_compacted"
)

// EffectiveAccountStatus 返回账号对管理端和路由可见的有效状态。
//
// 旧版本在 OAuth 刷新失败时只更新 token_status，导致 status 仍然是
// available。保留这个兼容判断可以让历史数据立即显示为失效，并避免
// 在调用方尚未完成迁移时继续把无效凭据当作可用账号。
func EffectiveAccountStatus(status, tokenStatus string) string {
	status = strings.ToLower(strings.TrimSpace(status))
	tokenStatus = strings.ToLower(strings.TrimSpace(tokenStatus))
	if status != AccountStatusDisabled && tokenStatus == TokenStatusInvalid {
		return AccountStatusInvalid
	}
	if (status == "" || status == AccountStatusAvailable) && tokenStatus == TokenStatusRefreshFailed {
		return AccountStatusInvalid
	}
	return status
}

// AccountTokenBlocksRouting 判断凭据状态是否已经明确不能用于路由。
func AccountTokenBlocksRouting(tokenStatus string) bool {
	switch strings.ToLower(strings.TrimSpace(tokenStatus)) {
	case TokenStatusRefreshFailed, TokenStatusInvalid:
		return true
	default:
		return false
	}
}
