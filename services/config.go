package services

import (
	"time"

	"github.com/spf13/cast"
	"github.com/wfu-work/nav-common-go-lib/global"
)

type FreeModelConfig struct {
	ProxyPrefix              string
	DefaultUpstreamBaseURL   string
	RequestTimeoutSeconds    int64
	StreamIdleTimeoutSeconds int64
	MaxRetries               int
	RoutingStrategy          string
	QuotaRefreshSeconds      int64
	CooldownSeconds          int64
	CleanupLogRetentionDays  int
	SecretKeyFile            string
	LogPromptContent         bool
}

func Config() FreeModelConfig {
	m := map[string]any{}
	if global.NAV_VIPER != nil {
		m = global.NAV_VIPER.GetStringMap("freeai")
		if len(m) == 0 {
			m = global.NAV_VIPER.GetStringMap("freemodel")
		}
	} else if global.NAV_CONFIG.Extras != nil {
		m = cast.ToStringMap(global.NAV_CONFIG.Extras["freeai"])
		if len(m) == 0 {
			m = cast.ToStringMap(global.NAV_CONFIG.Extras["freemodel"])
		}
	}
	return FreeModelConfig{
		ProxyPrefix:              stringDefault(cast.ToString(m["proxy-prefix"]), "/v1"),
		DefaultUpstreamBaseURL:   stringDefault(cast.ToString(m["default-upstream-base-url"]), "https://api.openai.com/v1"),
		RequestTimeoutSeconds:    int64Default(cast.ToInt64(m["request-timeout-seconds"]), 120),
		StreamIdleTimeoutSeconds: int64Default(cast.ToInt64(m["stream-idle-timeout-seconds"]), 60),
		MaxRetries:               intDefault(cast.ToInt(m["max-retries"]), 1),
		RoutingStrategy:          stringDefault(cast.ToString(m["routing-strategy"]), "weighted_round_robin"),
		QuotaRefreshSeconds:      int64Default(cast.ToInt64(m["quota-refresh-seconds"]), 300),
		CooldownSeconds:          int64Default(cast.ToInt64(m["cooldown-seconds"]), 300),
		CleanupLogRetentionDays:  intDefault(cast.ToInt(m["cleanup-log-retention-days"]), 30),
		SecretKeyFile:            stringDefault(cast.ToString(m["secret-key-file"]), "./data/master.key"),
		LogPromptContent:         cast.ToBool(m["log-prompt-content"]),
	}
}

func (c FreeModelConfig) RequestTimeout() time.Duration {
	return time.Duration(c.RequestTimeoutSeconds) * time.Second
}

func stringDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func intDefault(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}

func int64Default(v, def int64) int64 {
	if v == 0 {
		return def
	}
	return v
}
