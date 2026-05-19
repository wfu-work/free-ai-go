package domains

const (
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

	AuthTypeAPIKey        = "api_key"
	AuthTypeBearerToken   = "bearer_token"
	AuthTypeLoginCallback = "login_callback"

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
