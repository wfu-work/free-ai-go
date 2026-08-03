package domains

const (
	VendorOpenAI              = "openai"
	VendorGoogle              = "google"
	VendorAnthropic           = "anthropic"
	ProductCodex              = "codex"
	ProductGemini             = "gemini"
	ProductClaudeCode         = "claude_code"
	CredentialOAuth           = "oauth"
	ProtocolOpenAIResponses   = "openai_responses"
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

	TokenStatusActive        = "active"
	TokenStatusRefreshNeeded = "refresh_needed"
	TokenStatusRefreshFailed = "refresh_failed"
	TokenStatusInvalid       = "invalid"

	ErrorAuthFailed         = "auth_failed"
	ErrorRateLimited        = "rate_limited"
	ErrorQuotaExhausted     = "quota_exhausted"
	ErrorUpstreamTimeout    = "upstream_timeout"
	ErrorUpstream5xx        = "upstream_5xx"
	ErrorNetwork            = "network_error"
	ErrorModelNotSupported  = "model_not_supported"
	ErrorNoAvailableAccount = "no_available_account"
	ErrorPlatformKeyInvalid = "platform_key_invalid"
	ErrorPlatformKeyLimited = "platform_key_limited"
)
