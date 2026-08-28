package apis

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/wfu-work/free-ai-go/domains"
	"github.com/wfu-work/free-ai-go/services"
)

type ProxyApi struct{}

var proxyRequestGate = struct {
	sync.Mutex
	inFlight int
	notify   chan struct{}
}{notify: make(chan struct{})}

// Models 获取模型列表
// @Summary 获取OpenAI兼容模型列表
// @Description 获取当前平台密钥可访问的模型列表
// @Tags 代理模块
// @Accept json
// @Produce json
// @Success 200 {object} object
// @Router /v1/models [get]
func (a ProxyApi) Models(c *gin.Context) {
	start := time.Now()
	requestID := uuid.NewString()
	c.Header("X-Request-ID", requestID)
	path := c.Request.URL.Path
	key, err := services.PlatformKeyServiceApp.Verify(c.GetHeader("Authorization"))
	if err != nil {
		status := http.StatusUnauthorized
		code := "platform_key_invalid"
		if err.Error() == domains.ErrorPlatformKeyLimited {
			status = http.StatusTooManyRequests
			code = "platform_key_limited"
			c.Header("Retry-After", "60")
		}
		services.RequestLogServiceApp.Record(services.RequestLogInput{
			RequestID:    requestID,
			Method:       c.Request.Method,
			Path:         path,
			KeyPrefix:    services.PlatformKeyPrefixFromHeader(c.GetHeader("Authorization")),
			StatusCode:   status,
			ErrorType:    code,
			ErrorSummary: err.Error(),
			LatencyMs:    time.Since(start).Milliseconds(),
		})
		c.JSON(status, openAIError(code, err.Error()))
		return
	}
	models, err := services.ModelServiceApp.ListEnabled()
	if err != nil {
		services.RequestLogServiceApp.Record(services.RequestLogInput{
			RequestID:     requestID,
			Method:        c.Request.Method,
			Path:          path,
			PlatformKeyID: key.Guid,
			PlatformKey:   key.Name,
			KeyPrefix:     key.KeyPrefix,
			StatusCode:    http.StatusInternalServerError,
			ErrorType:     domains.ErrorInternal,
			ErrorSummary:  err.Error(),
			LatencyMs:     time.Since(start).Milliseconds(),
		})
		c.JSON(http.StatusInternalServerError, openAIError("server_error", err.Error()))
		return
	}
	data := make([]gin.H, 0, len(models))
	for _, model := range models {
		if !services.PlatformKeyServiceApp.ModelExposureAllowed(key, model) {
			continue
		}
		if !services.RouterServiceApp.HasAvailableAccount(model, key) {
			continue
		}
		for _, name := range services.ModelServiceApp.PublicNames(model) {
			data = append(data, gin.H{
				"id":       name,
				"object":   "model",
				"created":  model.Created,
				"owned_by": model.OwnedBy,
			})
		}
	}
	services.RequestLogServiceApp.Record(services.RequestLogInput{
		RequestID:     requestID,
		Method:        c.Request.Method,
		Path:          path,
		PlatformKeyID: key.Guid,
		PlatformKey:   key.Name,
		KeyPrefix:     key.KeyPrefix,
		StatusCode:    http.StatusOK,
		LatencyMs:     time.Since(start).Milliseconds(),
	})
	c.JSON(http.StatusOK, gin.H{"object": "list", "data": data})
}

// ChatCompletions OpenAI Chat Completions代理
// @Summary OpenAI Chat Completions代理
// @Description OpenAI兼容Chat Completions代理入口
// @Tags 代理模块
// @Accept json
// @Produce json
// @Router /v1/chat/completions [post]
func (a ProxyApi) ChatCompletions(c *gin.Context) {
	forwardProxy(c, "/v1/chat/completions")
}

// Responses OpenAI Responses代理
// @Summary OpenAI Responses代理
// @Description OpenAI兼容Responses代理入口
// @Tags 代理模块
// @Accept json
// @Produce json
// @Router /v1/responses [post]
func (a ProxyApi) Responses(c *gin.Context) {
	forwardProxy(c, "/v1/responses")
}

// ImageGenerations OpenAI Images API 兼容代理。
// @Summary OpenAI 图片生成代理
// @Description 使用独立 OpenAI Platform API Key 账号池生成图片
// @Tags 代理模块
// @Accept json
// @Produce json
// @Router /v1/images/generations [post]
func (a ProxyApi) ImageGenerations(c *gin.Context) {
	forwardProxy(c, "/v1/images/generations")
}

func forwardProxy(c *gin.Context, endpoint string) {
	requestID := uuid.NewString()
	c.Request.Header.Set("X-FreeAi-Request-ID", requestID)
	c.Header("X-Request-ID", requestID)
	cfg := services.Config()
	maxConcurrent := cfg.MaxConcurrentRequests
	if maxConcurrent <= 0 {
		maxConcurrent = 128
	}
	queueTimeout := time.Duration(cfg.OverloadQueueTimeoutMs) * time.Millisecond
	queueStartedAt := time.Now()
	if !acquireProxyRequestSlot(c.Request.Context(), maxConcurrent, queueTimeout) {
		gatewayQueueMs := time.Since(queueStartedAt).Milliseconds()
		c.Header("Retry-After", "1")
		recordProxyRejection(c, requestID, endpoint, http.StatusTooManyRequests, "server_overloaded", "too many concurrent requests", gatewayQueueMs)
		c.JSON(http.StatusTooManyRequests, openAIError("server_overloaded", "too many concurrent requests"))
		return
	}
	gatewayQueueMs := time.Since(queueStartedAt).Milliseconds()
	defer releaseProxyRequestSlot()

	maxBodyBytes := cfg.MaxRequestBodyBytes
	if maxBodyBytes <= 0 {
		maxBodyBytes = 8 * 1024 * 1024
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBodyBytes)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			recordProxyRejection(c, requestID, endpoint, http.StatusRequestEntityTooLarge, "request_too_large", err.Error(), gatewayQueueMs)
			c.JSON(http.StatusRequestEntityTooLarge, openAIError("request_too_large", "request body exceeds the configured limit"))
			return
		}
		recordProxyRejection(c, requestID, endpoint, http.StatusBadRequest, "invalid_request_error", err.Error(), gatewayQueueMs)
		c.JSON(http.StatusBadRequest, openAIError("invalid_request_error", err.Error()))
		return
	}
	model, stream, err := readProxyMetadata(body)
	if err != nil {
		recordProxyRejection(c, requestID, endpoint, http.StatusBadRequest, "invalid_request_error", err.Error(), gatewayQueueMs)
		c.JSON(http.StatusBadRequest, openAIError("invalid_request_error", err.Error()))
		return
	}
	if endpoint == "/v1/images/generations" && stream {
		message := "streaming is not supported by /v1/images/generations"
		recordProxyRejection(c, requestID, endpoint, http.StatusBadRequest, "invalid_request_error", message, gatewayQueueMs)
		c.JSON(http.StatusBadRequest, openAIError("invalid_request_error", message))
		return
	}
	if model != "" {
		c.Request.Header.Set("X-FreeAi-Model", model)
	}
	if stream {
		c.Writer.Header().Set("Content-Type", "text/event-stream")
		c.Writer.Header().Set("Cache-Control", "no-cache")
		c.Writer.Header().Set("Connection", "keep-alive")
	}
	out, err := proxyService.Handle(c.Request, c.Writer, endpoint, body, stream, services.ProxyIngressTiming{GatewayQueueMs: gatewayQueueMs})
	if err != nil {
		if stream && out.StatusCode >= 200 && out.StatusCode < 300 {
			return
		}
		status := out.StatusCode
		if status == 0 {
			status = http.StatusBadGateway
		}
		errorType := services.ClassifyProxyError(err)
		code := proxyErrorCode(status, errorType)
		// 上游错误也可能携带 Retry-After、请求追踪 ID 等端到端响应头。
		// 先复制安全响应头，再生成统一错误体，避免下游丢失官方的退避提示。
		contentType := ""
		if out.Header != nil {
			contentType = strings.ToLower(out.Header.Get("Content-Type"))
		}
		if out.Header != nil && (!stream || strings.HasPrefix(contentType, "application/json")) {
			copyProxyResponseHeaders(c.Writer.Header(), out.Header)
		}
		message := err.Error()
		if status == http.StatusInternalServerError {
			message = "internal server error"
		}
		if status == http.StatusTooManyRequests {
			c.Header("Retry-After", "60")
		}
		if status == http.StatusServiceUnavailable && c.Writer.Header().Get("Retry-After") == "" {
			// 本地没有候选账号或上游容量暂时不足时，给下游一个短退避，
			// 避免同一批请求立即打满所有重试机会。
			c.Header("Retry-After", "1")
		}
		c.JSON(status, openAIError(code, message))
		return
	}
	if out.Header != nil {
		copyProxyResponseHeaders(c.Writer.Header(), out.Header)
	}
	c.Header("X-Request-ID", requestID)
	status := out.StatusCode
	if status == 0 {
		status = http.StatusOK
	}
	if stream {
		if len(out.Body) > 0 {
			c.Writer.Header().Set("Content-Type", "application/json")
			c.Data(status, "application/json", out.Body)
		}
		return
	}
	c.Data(status, "application/json", out.Body)
}

func readProxyMetadata(body []byte) (string, bool, error) {
	if len(strings.TrimSpace(string(body))) == 0 {
		return "", false, errors.New("request body is required")
	}
	var payload struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", false, err
	}
	return strings.TrimSpace(payload.Model), payload.Stream, nil
}

func proxyErrorCode(status int, errorType string) string {
	switch status {
	case http.StatusBadRequest:
		return "invalid_request_error"
	case http.StatusUnauthorized:
		return "platform_key_invalid"
	case http.StatusForbidden:
		return "model_not_supported"
	case http.StatusTooManyRequests:
		return "platform_key_limited"
	case http.StatusServiceUnavailable:
		// 503 既可能来自本地路由阶段，也可能是账号上游返回的服务错误。
		// 只有明确分类为 no_available_account 时才保留该语义，避免把
		// OpenAI/上游代理的 503 误报成账号池为空。
		if errorType == domains.ErrorNoAvailableAccount {
			return errorType
		}
		if errorType == domains.ErrorUpstreamHTTP5xx || errorType == domains.ErrorUpstream5xx {
			return errorType
		}
		return "proxy_error"
	default:
		return "proxy_error"
	}
}

// copyProxyResponseHeaders 复制安全的端到端响应头，并过滤连接级和长度相关字段。
func copyProxyResponseHeaders(target, source http.Header) {
	hopByHop := map[string]bool{
		"Connection": true, "Keep-Alive": true, "Proxy-Authenticate": true,
		"Proxy-Authorization": true, "Te": true, "Trailer": true,
		"Transfer-Encoding": true, "Upgrade": true, "Content-Length": true,
	}
	for key, values := range source {
		canonical := http.CanonicalHeaderKey(key)
		if hopByHop[canonical] {
			continue
		}
		target.Del(canonical)
		for _, value := range values {
			target.Add(canonical, value)
		}
	}
}

func acquireProxyRequestSlot(ctx context.Context, max int, wait time.Duration) bool {
	deadline := time.Now().Add(wait)
	for {
		if ctx.Err() != nil {
			return false
		}
		proxyRequestGate.Lock()
		if proxyRequestGate.inFlight < max {
			proxyRequestGate.inFlight++
			proxyRequestGate.Unlock()
			return true
		}
		notify := proxyRequestGate.notify
		proxyRequestGate.Unlock()

		if wait <= 0 {
			return false
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false
		}
		timer := time.NewTimer(remaining)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return false
		case <-timer.C:
			return false
		case <-notify:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
	}
}

func releaseProxyRequestSlot() {
	proxyRequestGate.Lock()
	if proxyRequestGate.inFlight > 0 {
		proxyRequestGate.inFlight--
	}
	close(proxyRequestGate.notify)
	proxyRequestGate.notify = make(chan struct{})
	proxyRequestGate.Unlock()
}

func recordProxyRejection(c *gin.Context, requestID, endpoint string, status int, errorType, errorSummary string, gatewayQueueMs int64) {
	_ = services.RequestLogServiceApp.Record(services.RequestLogInput{
		RequestID: requestID, Method: c.Request.Method, Path: endpoint,
		KeyPrefix:  services.PlatformKeyPrefixFromHeader(c.GetHeader("Authorization")),
		StatusCode: status, ErrorType: errorType, ErrorSummary: errorSummary,
		GatewayQueueMs: gatewayQueueMs,
	})
}

func openAIError(code, message string) gin.H {
	return gin.H{"error": gin.H{
		"message": message,
		"type":    code,
		"code":    code,
	}}
}
