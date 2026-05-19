package services

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
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
	OpenAICallbackEnabled    bool
	OpenAICallbackAddr       string
	UpstreamProxyEnabled     bool
	UpstreamProxyURL         string
}

type GatewayProxyConfigInput struct {
	UpstreamProxyEnabled bool   `json:"upstreamProxyEnabled"`
	UpstreamProxyURL     string `json:"upstreamProxyUrl"`
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
	openAICallbackEnabled := true
	if _, ok := m["openai-callback-enabled"]; ok {
		openAICallbackEnabled = cast.ToBool(m["openai-callback-enabled"])
	}
	return FreeModelConfig{
		ProxyPrefix:              stringDefault(cast.ToString(m["proxy-prefix"]), "/v1"),
		DefaultUpstreamBaseURL:   stringDefault(cast.ToString(m["default-upstream-base-url"]), "https://api.openai.com/v1"),
		RequestTimeoutSeconds:    int64Default(cast.ToInt64(m["request-timeout-seconds"]), 120),
		StreamIdleTimeoutSeconds: int64Default(cast.ToInt64(m["stream-idle-timeout-seconds"]), 60),
		MaxRetries:               intDefault(cast.ToInt(m["max-retries"]), 1),
		RoutingStrategy:          stringDefault(cast.ToString(m["routing-strategy"]), "weighted_round_robin"),
		QuotaRefreshSeconds:      int64Default(cast.ToInt64(m["quota-refresh-seconds"]), 180),
		CooldownSeconds:          int64Default(cast.ToInt64(m["cooldown-seconds"]), 300),
		CleanupLogRetentionDays:  intDefault(cast.ToInt(m["cleanup-log-retention-days"]), 30),
		SecretKeyFile:            stringDefault(cast.ToString(m["secret-key-file"]), "./data/master.key"),
		LogPromptContent:         cast.ToBool(m["log-prompt-content"]),
		OpenAICallbackEnabled:    openAICallbackEnabled,
		OpenAICallbackAddr:       stringDefault(cast.ToString(m["openai-callback-addr"]), ":1455"),
		UpstreamProxyEnabled:     cast.ToBool(m["upstream-proxy-enabled"]),
		UpstreamProxyURL:         strings.TrimSpace(cast.ToString(m["upstream-proxy-url"])),
	}
}

func (c FreeModelConfig) RequestTimeout() time.Duration {
	return time.Duration(c.RequestTimeoutSeconds) * time.Second
}

func (c FreeModelConfig) EffectiveUpstreamProxyURL() string {
	if !c.UpstreamProxyEnabled {
		return ""
	}
	return strings.TrimSpace(c.UpstreamProxyURL)
}

func UpstreamHTTPClient() (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	proxyURL := Config().EffectiveUpstreamProxyURL()
	if proxyURL != "" {
		parsed, err := url.Parse(proxyURL)
		if err != nil {
			return nil, err
		}
		transport.Proxy = http.ProxyURL(parsed)
	}
	return &http.Client{Transport: transport}, nil
}

func GatewayProxyConfig() GatewayProxyConfigInput {
	cfg := Config()
	return GatewayProxyConfigInput{
		UpstreamProxyEnabled: cfg.UpstreamProxyEnabled,
		UpstreamProxyURL:     cfg.UpstreamProxyURL,
	}
}

func UpdateGatewayProxyConfig(input GatewayProxyConfigInput) (GatewayProxyConfigInput, error) {
	input.UpstreamProxyURL = strings.TrimSpace(input.UpstreamProxyURL)
	if input.UpstreamProxyEnabled {
		if input.UpstreamProxyURL == "" {
			return GatewayProxyConfigInput{}, errors.New("upstreamProxyUrl is required when upstream proxy is enabled")
		}
		if err := validateProxyURL(input.UpstreamProxyURL); err != nil {
			return GatewayProxyConfigInput{}, err
		}
	}
	global.Lock.Lock()
	defer global.Lock.Unlock()
	if global.NAV_VIPER != nil {
		global.NAV_VIPER.Set("freeai.upstream-proxy-enabled", input.UpstreamProxyEnabled)
		global.NAV_VIPER.Set("freeai.upstream-proxy-url", input.UpstreamProxyURL)
		if err := global.NAV_VIPER.WriteConfig(); err != nil {
			return GatewayProxyConfigInput{}, err
		}
	}
	return input, nil
}

func validateProxyURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil {
		return err
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https", "socks5":
	default:
		return errors.New("proxy scheme must be http, https, or socks5")
	}
	if parsed.Host == "" {
		return errors.New("proxy host is required")
	}
	return nil
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
