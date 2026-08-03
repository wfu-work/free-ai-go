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

type GatewayConfig struct {
	ProxyPrefix                string
	RequestTimeoutSeconds      int64
	StreamIdleTimeoutSeconds   int64
	MaxRetries                 int
	RoutingStrategy            string
	QuotaRefreshSeconds        int64
	CooldownSeconds            int64
	CleanupLogRetentionDays    int
	SecretKeyFile              string
	LogPromptContent           bool
	UpstreamProxyEnabled       bool
	UpstreamProxyURL           string
	MaxRequestBodyBytes        int64
	MaxConcurrentRequests      int
	QuotaDefaultReserveTokens  int64
	QuotaReservationTTLSeconds int64
}

type GatewayProxyConfigInput struct {
	ListenAddress               string `json:"listenAddress"`
	AccountSelectionStrategy    string `json:"accountSelectionStrategy"`
	Originator                  string `json:"originator"`
	Residency                   string `json:"residency"`
	UpstreamProxyEnabled        bool   `json:"upstreamProxyEnabled"`
	UpstreamProxyURL            string `json:"upstreamProxyUrl"`
	SSEKeepAliveMs              int64  `json:"sseKeepAliveMs"`
	UpstreamTimeoutMs           int64  `json:"upstreamTimeoutMs"`
	UpstreamStreamIdleTimeoutMs int64  `json:"upstreamStreamIdleTimeoutMs"`
}

const (
	systemConfigGroupGateway               = "gateway"
	systemConfigProxyPrefix                = "freeai.proxy-prefix"
	systemConfigRequestTimeoutSeconds      = "freeai.request-timeout-seconds"
	systemConfigStreamIdleTimeoutSeconds   = "freeai.stream-idle-timeout-seconds"
	systemConfigMaxRetries                 = "freeai.max-retries"
	systemConfigRoutingStrategy            = "freeai.routing-strategy"
	systemConfigQuotaRefreshSeconds        = "freeai.quota-refresh-seconds"
	systemConfigCooldownSeconds            = "freeai.cooldown-seconds"
	systemConfigCleanupLogRetentionDays    = "freeai.cleanup-log-retention-days"
	systemConfigSecretKeyFile              = "freeai.secret-key-file"
	systemConfigLogPromptContent           = "freeai.log-prompt-content"
	systemConfigUpstreamProxyEnabled       = "freeai.upstream-proxy-enabled"
	systemConfigUpstreamProxyURL           = "freeai.upstream-proxy-url"
	systemConfigMaxRequestBodyBytes        = "freeai.max-request-body-bytes"
	systemConfigMaxConcurrentRequests      = "freeai.max-concurrent-requests"
	systemConfigQuotaDefaultReserveTokens  = "freeai.quota-default-reserve-tokens"
	systemConfigQuotaReservationTTLSeconds = "freeai.quota-reservation-ttl-seconds"
	systemConfigGatewayListenAddress       = "gateway.listen-address"
	systemConfigGatewayAccountSelection    = "gateway.account-selection-strategy"
	systemConfigGatewayOriginator          = "gateway.originator"
	systemConfigGatewayResidency           = "gateway.residency"
	systemConfigGatewaySSEKeepAliveMs      = "gateway.sse-keep-alive-ms"
	systemConfigGatewayUpstreamTimeoutMs   = "gateway.upstream-timeout-ms"
	systemConfigGatewayStreamIdleTimeoutMs = "gateway.upstream-stream-idle-timeout-ms"
)

func Config() GatewayConfig {
	m := map[string]any{}
	if global.NAV_VIPER != nil {
		m = global.NAV_VIPER.GetStringMap("freeai")
	} else if global.NAV_CONFIG.Extras != nil {
		m = cast.ToStringMap(global.NAV_CONFIG.Extras["freeai"])
	}
	cfg := GatewayConfig{
		ProxyPrefix:                stringDefault(cast.ToString(m["proxy-prefix"]), "/v1"),
		RequestTimeoutSeconds:      int64Default(cast.ToInt64(m["request-timeout-seconds"]), 120),
		StreamIdleTimeoutSeconds:   int64Default(cast.ToInt64(m["stream-idle-timeout-seconds"]), 60),
		MaxRetries:                 intDefault(cast.ToInt(m["max-retries"]), 1),
		RoutingStrategy:            stringDefault(cast.ToString(m["routing-strategy"]), "weighted_round_robin"),
		QuotaRefreshSeconds:        int64Default(cast.ToInt64(m["quota-refresh-seconds"]), 180),
		CooldownSeconds:            int64Default(cast.ToInt64(m["cooldown-seconds"]), 300),
		CleanupLogRetentionDays:    intDefault(cast.ToInt(m["cleanup-log-retention-days"]), 30),
		SecretKeyFile:              stringDefault(cast.ToString(m["secret-key-file"]), "./data/master.key"),
		LogPromptContent:           cast.ToBool(m["log-prompt-content"]),
		UpstreamProxyEnabled:       cast.ToBool(m["upstream-proxy-enabled"]),
		UpstreamProxyURL:           strings.TrimSpace(cast.ToString(m["upstream-proxy-url"])),
		MaxRequestBodyBytes:        int64Default(cast.ToInt64(m["max-request-body-bytes"]), 8*1024*1024),
		MaxConcurrentRequests:      intDefault(cast.ToInt(m["max-concurrent-requests"]), 128),
		QuotaDefaultReserveTokens:  int64Default(cast.ToInt64(m["quota-default-reserve-tokens"]), 8192),
		QuotaReservationTTLSeconds: int64Default(cast.ToInt64(m["quota-reservation-ttl-seconds"]), 1800),
	}
	applySystemConfigOverrides(&cfg)
	return cfg
}

func applySystemConfigOverrides(cfg *GatewayConfig) {
	cfg.ProxyPrefix = SystemConfigServiceApp.GetString(systemConfigProxyPrefix, cfg.ProxyPrefix)
	cfg.RequestTimeoutSeconds = SystemConfigServiceApp.GetInt64(systemConfigRequestTimeoutSeconds, cfg.RequestTimeoutSeconds)
	cfg.StreamIdleTimeoutSeconds = SystemConfigServiceApp.GetInt64(systemConfigStreamIdleTimeoutSeconds, cfg.StreamIdleTimeoutSeconds)
	cfg.MaxRetries = SystemConfigServiceApp.GetInt(systemConfigMaxRetries, cfg.MaxRetries)
	cfg.RoutingStrategy = SystemConfigServiceApp.GetString(systemConfigRoutingStrategy, cfg.RoutingStrategy)
	cfg.QuotaRefreshSeconds = SystemConfigServiceApp.GetInt64(systemConfigQuotaRefreshSeconds, cfg.QuotaRefreshSeconds)
	cfg.CooldownSeconds = SystemConfigServiceApp.GetInt64(systemConfigCooldownSeconds, cfg.CooldownSeconds)
	cfg.CleanupLogRetentionDays = SystemConfigServiceApp.GetInt(systemConfigCleanupLogRetentionDays, cfg.CleanupLogRetentionDays)
	cfg.SecretKeyFile = SystemConfigServiceApp.GetString(systemConfigSecretKeyFile, cfg.SecretKeyFile)
	cfg.LogPromptContent = SystemConfigServiceApp.GetBool(systemConfigLogPromptContent, cfg.LogPromptContent)
	cfg.UpstreamProxyEnabled = SystemConfigServiceApp.GetBool(systemConfigUpstreamProxyEnabled, cfg.UpstreamProxyEnabled)
	cfg.UpstreamProxyURL = strings.TrimSpace(SystemConfigServiceApp.GetString(systemConfigUpstreamProxyURL, cfg.UpstreamProxyURL))
	cfg.MaxRequestBodyBytes = SystemConfigServiceApp.GetInt64(systemConfigMaxRequestBodyBytes, cfg.MaxRequestBodyBytes)
	cfg.MaxConcurrentRequests = SystemConfigServiceApp.GetInt(systemConfigMaxConcurrentRequests, cfg.MaxConcurrentRequests)
	cfg.QuotaDefaultReserveTokens = SystemConfigServiceApp.GetInt64(systemConfigQuotaDefaultReserveTokens, cfg.QuotaDefaultReserveTokens)
	cfg.QuotaReservationTTLSeconds = SystemConfigServiceApp.GetInt64(systemConfigQuotaReservationTTLSeconds, cfg.QuotaReservationTTLSeconds)
}

func (c GatewayConfig) RequestTimeout() time.Duration {
	return time.Duration(c.RequestTimeoutSeconds) * time.Second
}

func (c GatewayConfig) EffectiveUpstreamProxyURL() string {
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
		ListenAddress:               SystemConfigServiceApp.GetString(systemConfigGatewayListenAddress, "127.0.0.1"),
		AccountSelectionStrategy:    gatewayAccountSelectionStrategy(cfg.RoutingStrategy),
		Originator:                  SystemConfigServiceApp.GetString(systemConfigGatewayOriginator, "codex_cli_rs"),
		Residency:                   SystemConfigServiceApp.GetString(systemConfigGatewayResidency, ""),
		UpstreamProxyEnabled:        cfg.UpstreamProxyEnabled,
		UpstreamProxyURL:            cfg.UpstreamProxyURL,
		SSEKeepAliveMs:              SystemConfigServiceApp.GetInt64(systemConfigGatewaySSEKeepAliveMs, 15000),
		UpstreamTimeoutMs:           SystemConfigServiceApp.GetInt64(systemConfigGatewayUpstreamTimeoutMs, cfg.RequestTimeoutSeconds*1000),
		UpstreamStreamIdleTimeoutMs: SystemConfigServiceApp.GetInt64(systemConfigGatewayStreamIdleTimeoutMs, cfg.StreamIdleTimeoutSeconds*1000),
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
	if err := SystemConfigServiceApp.SetBool(systemConfigGroupGateway, systemConfigUpstreamProxyEnabled, input.UpstreamProxyEnabled, "上游代理开关"); err != nil {
		return GatewayProxyConfigInput{}, err
	}
	if err := SystemConfigServiceApp.SetString(systemConfigGroupGateway, systemConfigUpstreamProxyURL, input.UpstreamProxyURL, "上游代理地址"); err != nil {
		return GatewayProxyConfigInput{}, err
	}
	if err := updateGatewayRuntimeConfig(input); err != nil {
		return GatewayProxyConfigInput{}, err
	}
	return GatewayProxyConfig(), nil
}

func updateGatewayRuntimeConfig(input GatewayProxyConfigInput) error {
	values := []struct {
		key    string
		value  string
		remark string
	}{
		{systemConfigGatewayListenAddress, normalizeListenAddress(input.ListenAddress), "网关监听地址"},
		{systemConfigGatewayAccountSelection, stringDefault(strings.TrimSpace(input.AccountSelectionStrategy), "ordered"), "账号选择策略"},
		{systemConfigRoutingStrategy, routingStrategyFromGateway(input.AccountSelectionStrategy), "账号池路由策略"},
		{systemConfigGatewayOriginator, stringDefault(strings.TrimSpace(input.Originator), "codex_cli_rs"), "上游 Originator"},
		{systemConfigGatewayResidency, strings.TrimSpace(input.Residency), "区域驻留要求"},
	}
	for _, item := range values {
		if err := SystemConfigServiceApp.SetString(systemConfigGroupGateway, item.key, item.value, item.remark); err != nil {
			return err
		}
	}
	if err := SystemConfigServiceApp.SetInt64(systemConfigGroupGateway, systemConfigGatewaySSEKeepAliveMs, int64Default(input.SSEKeepAliveMs, 15000), "SSE 保活间隔"); err != nil {
		return err
	}
	if err := SystemConfigServiceApp.SetInt64(systemConfigGroupGateway, systemConfigGatewayUpstreamTimeoutMs, input.UpstreamTimeoutMs, "上游总超时"); err != nil {
		return err
	}
	requestTimeoutSeconds := input.UpstreamTimeoutMs / 1000
	if requestTimeoutSeconds <= 0 {
		requestTimeoutSeconds = 120
	}
	if err := SystemConfigServiceApp.SetInt64(systemConfigGroupGateway, systemConfigRequestTimeoutSeconds, requestTimeoutSeconds, "上游请求超时秒数"); err != nil {
		return err
	}
	streamIdleMs := int64Default(input.UpstreamStreamIdleTimeoutMs, 1800000)
	if err := SystemConfigServiceApp.SetInt64(systemConfigGroupGateway, systemConfigGatewayStreamIdleTimeoutMs, streamIdleMs, "上游流式空闲超时"); err != nil {
		return err
	}
	return SystemConfigServiceApp.SetInt64(systemConfigGroupGateway, systemConfigStreamIdleTimeoutSeconds, streamIdleMs/1000, "上游流式空闲超时秒数")
}

func gatewayAccountSelectionStrategy(value string) string {
	if strings.TrimSpace(value) == "round_robin" || strings.TrimSpace(value) == "weighted_round_robin" {
		return "round_robin"
	}
	return "ordered"
}

func routingStrategyFromGateway(value string) string {
	if strings.TrimSpace(value) == "round_robin" {
		return "weighted_round_robin"
	}
	return "priority_first"
}

func normalizeListenAddress(value string) string {
	if strings.TrimSpace(value) == "0.0.0.0" {
		return "0.0.0.0"
	}
	return "127.0.0.1"
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
