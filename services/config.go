package services

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
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
	OverloadQueueTimeoutMs     int64
	QuotaDefaultReserveTokens  int64
	QuotaReservationTTLSeconds int64
	ContextCompactionEnabled   bool
	ContextCompactionThreshold int64
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
	MaxConcurrentRequests       *int   `json:"maxConcurrentRequests,omitempty"`
	MaxRequestBodyMiB           *int64 `json:"maxRequestBodyMiB,omitempty"`
	MaxRetries                  *int   `json:"maxRetries,omitempty"`
	OverloadQueueTimeoutMs      *int64 `json:"overloadQueueTimeoutMs,omitempty"`
	ContextCompactionEnabled    bool   `json:"contextCompactionEnabled"`
	ContextCompactionThreshold  int64  `json:"contextCompactionThresholdTokens"`
}

type gatewayCapacityConfig struct {
	MaxConcurrentRequests  int
	MaxRequestBodyBytes    int64
	MaxRetries             int
	OverloadQueueTimeoutMs int64
}

var upstreamHTTPClientCache = struct {
	sync.Mutex
	proxyURL  string
	client    *http.Client
	transport *http.Transport
}{}

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
	systemConfigOverloadQueueTimeoutMs     = "freeai.overload-queue-timeout-ms"
	systemConfigQuotaDefaultReserveTokens  = "freeai.quota-default-reserve-tokens"
	systemConfigQuotaReservationTTLSeconds = "freeai.quota-reservation-ttl-seconds"
	systemConfigGatewayListenAddress       = "gateway.listen-address"
	systemConfigGatewayAccountSelection    = "gateway.account-selection-strategy"
	systemConfigGatewayOriginator          = "gateway.originator"
	systemConfigGatewayResidency           = "gateway.residency"
	systemConfigGatewaySSEKeepAliveMs      = "gateway.sse-keep-alive-ms"
	systemConfigGatewayUpstreamTimeoutMs   = "gateway.upstream-timeout-ms"
	systemConfigGatewayStreamIdleTimeoutMs = "gateway.upstream-stream-idle-timeout-ms"
	systemConfigContextCompactionEnabled   = "gateway.context-compaction-enabled"
	systemConfigContextCompactionThreshold = "gateway.context-compaction-threshold-tokens"
)

const (
	gatewayStrategyOrdered         = "ordered"
	gatewayStrategyAdaptive        = "round_robin" // 兼容旧版管理端：round_robin 一直表示自适应均衡。
	gatewayStrategyRoundRobin      = "equal_round_robin"
	gatewayStrategyWeighted        = "weighted"
	gatewayStrategyQuotaFirst      = "quota_first"
	gatewayStrategyLeastRecentUsed = "least_recently_used"
	gatewayStrategySessionAffinity = "session_affinity"
	gatewayStrategyQuotaAdaptive   = "quota_adaptive"
)

const (
	defaultContextCompactionThreshold = int64(100000)
	minContextCompactionThreshold     = int64(80000)
	maxContextCompactionThreshold     = int64(120000)
)

func Config() GatewayConfig {
	m := map[string]any{}
	if global.NAV_VIPER != nil {
		m = global.NAV_VIPER.GetStringMap("freeai")
	} else if global.NAV_CONFIG.Extras != nil {
		m = cast.ToStringMap(global.NAV_CONFIG.Extras["freeai"])
	}
	contextCompactionEnabled := true
	if value, ok := m["context-compaction-enabled"]; ok {
		contextCompactionEnabled = cast.ToBool(value)
	}
	cfg := GatewayConfig{
		ProxyPrefix:                stringDefault(cast.ToString(m["proxy-prefix"]), "/v1"),
		RequestTimeoutSeconds:      int64Default(cast.ToInt64(m["request-timeout-seconds"]), 120),
		StreamIdleTimeoutSeconds:   int64Default(cast.ToInt64(m["stream-idle-timeout-seconds"]), 60),
		MaxRetries:                 intDefault(cast.ToInt(m["max-retries"]), 1),
		RoutingStrategy:            stringDefault(cast.ToString(m["routing-strategy"]), routingStrategyAdaptiveWeighted),
		QuotaRefreshSeconds:        int64Default(cast.ToInt64(m["quota-refresh-seconds"]), 180),
		CooldownSeconds:            int64Default(cast.ToInt64(m["cooldown-seconds"]), 300),
		CleanupLogRetentionDays:    normalizeCleanupLogRetentionDays(intDefault(cast.ToInt(m["cleanup-log-retention-days"]), MaxUsageRetentionDays)),
		SecretKeyFile:              stringDefault(cast.ToString(m["secret-key-file"]), "./data/master.key"),
		LogPromptContent:           cast.ToBool(m["log-prompt-content"]),
		UpstreamProxyEnabled:       cast.ToBool(m["upstream-proxy-enabled"]),
		UpstreamProxyURL:           strings.TrimSpace(cast.ToString(m["upstream-proxy-url"])),
		MaxRequestBodyBytes:        int64Default(cast.ToInt64(m["max-request-body-bytes"]), 8*1024*1024),
		MaxConcurrentRequests:      intDefault(cast.ToInt(m["max-concurrent-requests"]), 128),
		OverloadQueueTimeoutMs:     cast.ToInt64(m["overload-queue-timeout-ms"]),
		QuotaDefaultReserveTokens:  int64Default(cast.ToInt64(m["quota-default-reserve-tokens"]), 8192),
		QuotaReservationTTLSeconds: int64Default(cast.ToInt64(m["quota-reservation-ttl-seconds"]), 1800),
		ContextCompactionEnabled:   contextCompactionEnabled,
		ContextCompactionThreshold: int64Default(cast.ToInt64(m["context-compaction-threshold-tokens"]), defaultContextCompactionThreshold),
	}
	applySystemConfigOverrides(&cfg)
	cfg.CleanupLogRetentionDays = normalizeCleanupLogRetentionDays(cfg.CleanupLogRetentionDays)
	cfg.ContextCompactionThreshold = normalizeContextCompactionThreshold(cfg.ContextCompactionThreshold)
	return cfg
}

func normalizeCleanupLogRetentionDays(days int) int {
	if days <= 0 || days > MaxUsageRetentionDays {
		return MaxUsageRetentionDays
	}
	return days
}

func applySystemConfigOverrides(cfg *GatewayConfig) {
	cfg.ProxyPrefix = SystemConfigServiceApp.GetString(systemConfigProxyPrefix, cfg.ProxyPrefix)
	cfg.RequestTimeoutSeconds = SystemConfigServiceApp.GetInt64(systemConfigRequestTimeoutSeconds, cfg.RequestTimeoutSeconds)
	// gateway.upstream-timeout-ms 是管理界面的当前配置源；存在时覆盖旧的秒级兼容项。
	if _, ok := SystemConfigServiceApp.Get(systemConfigGatewayUpstreamTimeoutMs); ok {
		timeoutMs := SystemConfigServiceApp.GetInt64(systemConfigGatewayUpstreamTimeoutMs, cfg.RequestTimeoutSeconds*1000)
		cfg.RequestTimeoutSeconds = timeoutMillisecondsToSeconds(timeoutMs)
	}
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
	cfg.OverloadQueueTimeoutMs = SystemConfigServiceApp.GetInt64(systemConfigOverloadQueueTimeoutMs, cfg.OverloadQueueTimeoutMs)
	cfg.QuotaDefaultReserveTokens = SystemConfigServiceApp.GetInt64(systemConfigQuotaDefaultReserveTokens, cfg.QuotaDefaultReserveTokens)
	cfg.QuotaReservationTTLSeconds = SystemConfigServiceApp.GetInt64(systemConfigQuotaReservationTTLSeconds, cfg.QuotaReservationTTLSeconds)
	cfg.ContextCompactionEnabled = SystemConfigServiceApp.GetBool(systemConfigContextCompactionEnabled, cfg.ContextCompactionEnabled)
	cfg.ContextCompactionThreshold = SystemConfigServiceApp.GetInt64(systemConfigContextCompactionThreshold, cfg.ContextCompactionThreshold)
}

func (c GatewayConfig) RequestTimeout() time.Duration {
	return time.Duration(c.RequestTimeoutSeconds) * time.Second
}

func (c GatewayConfig) StreamIdleTimeout() time.Duration {
	return time.Duration(c.StreamIdleTimeoutSeconds) * time.Second
}

// contextWithOptionalTimeout 创建可取消的请求上下文；timeout <= 0 表示不设置截止时间。
func contextWithOptionalTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, timeout)
}

func (c GatewayConfig) EffectiveUpstreamProxyURL() string {
	if !c.UpstreamProxyEnabled {
		return ""
	}
	return strings.TrimSpace(c.UpstreamProxyURL)
}

func UpstreamHTTPClient() (*http.Client, error) {
	return upstreamHTTPClientForProxy(Config().EffectiveUpstreamProxyURL())
}

func upstreamHTTPClientForProxy(proxyURL string) (*http.Client, error) {
	proxyURL = strings.TrimSpace(proxyURL)
	var parsedProxy *url.URL
	if proxyURL != "" {
		parsed, err := url.Parse(proxyURL)
		if err != nil {
			return nil, err
		}
		parsedProxy = parsed
	}

	upstreamHTTPClientCache.Lock()
	defer upstreamHTTPClientCache.Unlock()
	if upstreamHTTPClientCache.client != nil && upstreamHTTPClientCache.proxyURL == proxyURL {
		return upstreamHTTPClientCache.client, nil
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.ForceAttemptHTTP2 = true
	transport.DisableKeepAlives = false
	transport.MaxIdleConns = 256
	transport.MaxIdleConnsPerHost = 128
	transport.IdleConnTimeout = 90 * time.Second
	if parsedProxy != nil {
		transport.Proxy = http.ProxyURL(parsedProxy)
	}
	client := &http.Client{Transport: transport}
	if upstreamHTTPClientCache.transport != nil {
		upstreamHTTPClientCache.transport.CloseIdleConnections()
	}
	upstreamHTTPClientCache.proxyURL = proxyURL
	upstreamHTTPClientCache.client = client
	upstreamHTTPClientCache.transport = transport
	return client, nil
}

func GatewayProxyConfig() GatewayProxyConfigInput {
	cfg := Config()
	maxRequestBodyMiB := bytesToMiB(cfg.MaxRequestBodyBytes)
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
		MaxConcurrentRequests:       intPtr(cfg.MaxConcurrentRequests),
		MaxRequestBodyMiB:           int64Ptr(maxRequestBodyMiB),
		MaxRetries:                  intPtr(cfg.MaxRetries),
		OverloadQueueTimeoutMs:      int64Ptr(cfg.OverloadQueueTimeoutMs),
		ContextCompactionEnabled:    cfg.ContextCompactionEnabled,
		ContextCompactionThreshold:  cfg.ContextCompactionThreshold,
	}
}

func UpdateGatewayProxyConfig(input GatewayProxyConfigInput) (GatewayProxyConfigInput, error) {
	input.AccountSelectionStrategy = gatewayAccountSelectionStrategy(routingStrategyFromGateway(input.AccountSelectionStrategy))
	input.UpstreamProxyURL = strings.TrimSpace(input.UpstreamProxyURL)
	if input.ContextCompactionThreshold == 0 {
		input.ContextCompactionThreshold = defaultContextCompactionThreshold
	}
	if input.UpstreamTimeoutMs < 0 {
		return GatewayProxyConfigInput{}, errors.New("upstreamTimeoutMs must be greater than or equal to 0")
	}
	if input.ContextCompactionThreshold < minContextCompactionThreshold || input.ContextCompactionThreshold > maxContextCompactionThreshold {
		return GatewayProxyConfigInput{}, errors.New("contextCompactionThresholdTokens must be between 80000 and 120000")
	}
	capacity, err := resolveGatewayCapacity(input, Config())
	if err != nil {
		return GatewayProxyConfigInput{}, err
	}
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
	if err := SystemConfigServiceApp.SetBool(systemConfigGroupGateway, systemConfigContextCompactionEnabled, input.ContextCompactionEnabled, "自动上下文压缩开关"); err != nil {
		return GatewayProxyConfigInput{}, err
	}
	if err := SystemConfigServiceApp.SetInt64(systemConfigGroupGateway, systemConfigContextCompactionThreshold, input.ContextCompactionThreshold, "自动上下文压缩 Token 阈值"); err != nil {
		return GatewayProxyConfigInput{}, err
	}
	if err := updateGatewayRuntimeConfig(input, capacity); err != nil {
		return GatewayProxyConfigInput{}, err
	}
	return GatewayProxyConfig(), nil
}

func updateGatewayRuntimeConfig(input GatewayProxyConfigInput, capacity gatewayCapacityConfig) error {
	values := []struct {
		key    string
		value  string
		remark string
	}{
		{systemConfigGatewayListenAddress, normalizeListenAddress(input.ListenAddress), "网关监听地址"},
		{systemConfigGatewayAccountSelection, stringDefault(strings.TrimSpace(input.AccountSelectionStrategy), gatewayStrategyOrdered), "账号选择策略"},
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
	if err := SystemConfigServiceApp.SetInt64(systemConfigGroupGateway, systemConfigGatewayUpstreamTimeoutMs, input.UpstreamTimeoutMs, "上游首响应超时"); err != nil {
		return err
	}
	requestTimeoutSeconds := timeoutMillisecondsToSeconds(input.UpstreamTimeoutMs)
	if err := SystemConfigServiceApp.SetInt64(systemConfigGroupGateway, systemConfigRequestTimeoutSeconds, requestTimeoutSeconds, "非流式总超时 / 流式首响应超时秒数"); err != nil {
		return err
	}
	streamIdleMs := int64Default(input.UpstreamStreamIdleTimeoutMs, 1800000)
	if err := SystemConfigServiceApp.SetInt64(systemConfigGroupGateway, systemConfigGatewayStreamIdleTimeoutMs, streamIdleMs, "上游流式空闲超时"); err != nil {
		return err
	}
	if err := SystemConfigServiceApp.SetInt64(systemConfigGroupGateway, systemConfigStreamIdleTimeoutSeconds, streamIdleMs/1000, "上游流式空闲超时秒数"); err != nil {
		return err
	}
	if err := SystemConfigServiceApp.SetInt(systemConfigGroupGateway, systemConfigMaxConcurrentRequests, capacity.MaxConcurrentRequests, "网关最大并发请求数"); err != nil {
		return err
	}
	if err := SystemConfigServiceApp.SetInt64(systemConfigGroupGateway, systemConfigMaxRequestBodyBytes, capacity.MaxRequestBodyBytes, "网关最大请求体字节数"); err != nil {
		return err
	}
	if err := SystemConfigServiceApp.SetInt(systemConfigGroupGateway, systemConfigMaxRetries, capacity.MaxRetries, "上游故障切换重试次数"); err != nil {
		return err
	}
	return SystemConfigServiceApp.SetInt64(systemConfigGroupGateway, systemConfigOverloadQueueTimeoutMs, capacity.OverloadQueueTimeoutMs, "网关过载排队时间毫秒")
}

func timeoutMillisecondsToSeconds(timeoutMs int64) int64 {
	if timeoutMs <= 0 {
		return 0
	}
	seconds := timeoutMs / 1000
	if seconds == 0 {
		return 1
	}
	return seconds
}

func normalizeContextCompactionThreshold(value int64) int64 {
	if value < minContextCompactionThreshold || value > maxContextCompactionThreshold {
		return defaultContextCompactionThreshold
	}
	return value
}

func resolveGatewayCapacity(input GatewayProxyConfigInput, fallback GatewayConfig) (gatewayCapacityConfig, error) {
	capacity := gatewayCapacityConfig{
		MaxConcurrentRequests:  fallback.MaxConcurrentRequests,
		MaxRequestBodyBytes:    fallback.MaxRequestBodyBytes,
		MaxRetries:             fallback.MaxRetries,
		OverloadQueueTimeoutMs: fallback.OverloadQueueTimeoutMs,
	}
	if input.MaxConcurrentRequests != nil {
		capacity.MaxConcurrentRequests = *input.MaxConcurrentRequests
	}
	if input.MaxRequestBodyMiB != nil {
		capacity.MaxRequestBodyBytes = *input.MaxRequestBodyMiB * 1024 * 1024
	}
	if input.MaxRetries != nil {
		capacity.MaxRetries = *input.MaxRetries
	}
	if input.OverloadQueueTimeoutMs != nil {
		capacity.OverloadQueueTimeoutMs = *input.OverloadQueueTimeoutMs
	}

	switch {
	case capacity.MaxConcurrentRequests < 1 || capacity.MaxConcurrentRequests > 4096:
		return gatewayCapacityConfig{}, errors.New("maxConcurrentRequests must be between 1 and 4096")
	case capacity.MaxRequestBodyBytes < 1024*1024 || capacity.MaxRequestBodyBytes > 512*1024*1024:
		return gatewayCapacityConfig{}, errors.New("maxRequestBodyMiB must be between 1 and 512")
	case capacity.MaxRetries < 0 || capacity.MaxRetries > 5:
		return gatewayCapacityConfig{}, errors.New("maxRetries must be between 0 and 5")
	case capacity.OverloadQueueTimeoutMs < 0 || capacity.OverloadQueueTimeoutMs > 60000:
		return gatewayCapacityConfig{}, errors.New("overloadQueueTimeoutMs must be between 0 and 60000")
	}
	return capacity, nil
}

func bytesToMiB(value int64) int64 {
	if value <= 0 {
		return 1
	}
	const mib = int64(1024 * 1024)
	return (value + mib - 1) / mib
}

func intPtr(value int) *int       { return &value }
func int64Ptr(value int64) *int64 { return &value }

func gatewayAccountSelectionStrategy(value string) string {
	switch strings.TrimSpace(value) {
	case routingStrategyAdaptiveWeighted:
		return gatewayStrategyAdaptive
	case routingStrategyRoundRobin:
		return gatewayStrategyRoundRobin
	case routingStrategyStaticWeightedRoundRobin:
		return gatewayStrategyWeighted
	case routingStrategyMostQuotaRemaining:
		return gatewayStrategyQuotaFirst
	case routingStrategyLeastRecentlyUsed:
		return gatewayStrategyLeastRecentUsed
	case routingStrategySessionAffinity:
		return gatewayStrategySessionAffinity
	case routingStrategyQuotaAwareAdaptive:
		return gatewayStrategyQuotaAdaptive
	default:
		return gatewayStrategyOrdered
	}
}

func routingStrategyFromGateway(value string) string {
	switch strings.TrimSpace(value) {
	case gatewayStrategyAdaptive, "adaptive", routingStrategyAdaptiveWeighted:
		return routingStrategyAdaptiveWeighted
	case gatewayStrategyRoundRobin:
		return routingStrategyRoundRobin
	case gatewayStrategyWeighted, routingStrategyStaticWeightedRoundRobin:
		return routingStrategyStaticWeightedRoundRobin
	case gatewayStrategyQuotaFirst, routingStrategyMostQuotaRemaining:
		return routingStrategyMostQuotaRemaining
	case gatewayStrategyLeastRecentUsed:
		return routingStrategyLeastRecentlyUsed
	case gatewayStrategySessionAffinity:
		return routingStrategySessionAffinity
	case gatewayStrategyQuotaAdaptive, routingStrategyQuotaAwareAdaptive:
		return routingStrategyQuotaAwareAdaptive
	default:
		return routingStrategyPriorityFirst
	}
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
