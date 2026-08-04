package services

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/wfu-work/free-ai-go/domains"
)

type ProxyService struct{}

var ProxyServiceApp = ProxyService{}

type ProxyOutput struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

func (s ProxyService) Handle(r *http.Request, w io.Writer, endpoint string, body []byte, stream bool) (ProxyOutput, error) {
	start := time.Now()
	requestID := strings.TrimSpace(r.Header.Get("X-FreeAi-Request-ID"))
	logMeta := requestLogMeta(r, endpoint, body)
	modelName := r.URL.Query().Get("model")
	if modelName == "" {
		modelName = r.Header.Get("X-FreeAi-Model")
	}
	if modelName == "" {
		modelName = logMeta.Model
	}
	platformKey, err := PlatformKeyServiceApp.Verify(r.Header.Get("Authorization"))
	if err != nil {
		status := http.StatusUnauthorized
		errorType := domains.ErrorPlatformKeyInvalid
		if err.Error() == domains.ErrorPlatformKeyLimited {
			status = http.StatusTooManyRequests
			errorType = domains.ErrorPlatformKeyLimited
		}
		RequestLogServiceApp.Record(RequestLogInput{
			RequestID:       requestID,
			Method:          logMeta.Method,
			Path:            logMeta.Path,
			KeyPrefix:       PlatformKeyPrefixFromHeader(r.Header.Get("Authorization")),
			Model:           modelName,
			ReasoningEffort: logMeta.ReasoningEffort,
			ServiceTier:     logMeta.ServiceTier,
			StatusCode:      status,
			ErrorType:       errorType,
			LatencyMs:       time.Since(start).Milliseconds(),
		})
		return ProxyOutput{StatusCode: status}, err
	}
	if platformKey.BoundModel != "" {
		modelName = platformKey.BoundModel
		logMeta.Model = modelName
	}
	if modelName == "" {
		RequestLogServiceApp.Record(RequestLogInput{
			RequestID:       requestID,
			PlatformKeyID:   platformKey.Guid,
			PlatformKey:     platformKey.Name,
			KeyPrefix:       platformKey.KeyPrefix,
			Method:          logMeta.Method,
			Path:            logMeta.Path,
			Model:           modelName,
			ReasoningEffort: firstNonEmpty(logMeta.ReasoningEffort, platformKey.ReasoningEffort),
			ServiceTier:     firstNonEmpty(logMeta.ServiceTier, platformKey.ServiceTier),
			StatusCode:      http.StatusBadRequest,
			ErrorType:       domains.ErrorModelNotSupported,
			LatencyMs:       time.Since(start).Milliseconds(),
		})
		return ProxyOutput{StatusCode: http.StatusBadRequest}, errors.New("model is required")
	}
	if !PlatformKeyServiceApp.ModelAllowed(platformKey, modelName) {
		if model, findErr := ModelServiceApp.Find(modelName); findErr != nil || !PlatformKeyServiceApp.ModelExposureAllowed(platformKey, model) {
			RequestLogServiceApp.Record(RequestLogInput{
				RequestID:       requestID,
				PlatformKeyID:   platformKey.Guid,
				PlatformKey:     platformKey.Name,
				KeyPrefix:       platformKey.KeyPrefix,
				Method:          logMeta.Method,
				Path:            logMeta.Path,
				Model:           modelName,
				ReasoningEffort: firstNonEmpty(logMeta.ReasoningEffort, platformKey.ReasoningEffort),
				ServiceTier:     firstNonEmpty(logMeta.ServiceTier, platformKey.ServiceTier),
				StatusCode:      http.StatusForbidden,
				ErrorType:       domains.ErrorModelNotSupported,
				LatencyMs:       time.Since(start).Milliseconds(),
			})
			return ProxyOutput{StatusCode: http.StatusForbidden}, errors.New(domains.ErrorModelNotSupported)
		}
	}
	body = applyPlatformKeyRequestOverrides(body, platformKey, modelName)
	logMeta = requestLogMeta(r, endpoint, body)
	logMeta.Model = modelName
	reservation, err := PlatformKeyServiceApp.ReserveTokens(platformKey, requestID, body)
	if err != nil {
		status := http.StatusTooManyRequests
		errorType := domains.ErrorPlatformKeyLimited
		if err.Error() != domains.ErrorPlatformKeyLimited {
			status = http.StatusInternalServerError
			errorType = "server_error"
		}
		RequestLogServiceApp.Record(RequestLogInput{
			RequestID: requestID, PlatformKeyID: platformKey.Guid, PlatformKey: platformKey.Name,
			KeyPrefix: platformKey.KeyPrefix, Method: logMeta.Method, Path: logMeta.Path, Model: modelName,
			ReasoningEffort: firstNonEmpty(logMeta.ReasoningEffort, platformKey.ReasoningEffort),
			ServiceTier:     firstNonEmpty(logMeta.ServiceTier, platformKey.ServiceTier), StatusCode: status,
			ErrorType: errorType, LatencyMs: time.Since(start).Milliseconds(),
		})
		return ProxyOutput{StatusCode: status}, err
	}
	settledReservation := false
	defer func() {
		if !settledReservation {
			_ = PlatformKeyServiceApp.FinalizeTokens(platformKey.Guid, reservation, 0, 0)
		}
	}()

	maxAttempts := Config().MaxRetries + 1
	if maxAttempts <= 0 {
		maxAttempts = 1
	}
	excluded := map[string]bool{}
	switchReasons := make([]string, 0, maxAttempts)
	var lastOutput ProxyOutput
	var lastErr error
	var lastResult *ProxyResult
	var lastSelection RouteSelection
	for attempt := 0; attempt < maxAttempts; attempt++ {
		selection, err := RouterServiceApp.SelectForKey(modelName, excluded, platformKey)
		if err != nil {
			lastErr = err
			status := http.StatusServiceUnavailable
			if err.Error() == domains.ErrorModelNotSupported {
				status = http.StatusBadRequest
			}
			lastOutput = ProxyOutput{StatusCode: status}
			break
		}
		lastSelection = selection
		excluded[selection.Account.Guid] = true
		result, output, err := s.callUpstream(r, w, endpoint, body, stream, selection)
		lastResult = result
		lastOutput = output
		lastErr = err
		if result != nil && result.ErrorType != "" {
			QuotaServiceApp.ApplyError(selection.Account.Guid, result.ErrorType)
		}
		if result != nil && len(result.Header) > 0 {
			_, _ = QuotaServiceApp.SampleHeaders(selection.Account.Guid, "response_header", result.Header)
		}
		if err == nil && (result == nil || result.ErrorType == "") {
			_ = AccountServiceApp.MarkUsed(selection.Account.Guid)
			break
		}
		if !shouldRetry(result, err, stream) || attempt == maxAttempts-1 {
			break
		}
		reason := "upstream_error"
		if result != nil && result.ErrorType != "" {
			reason = result.ErrorType
		} else if err != nil {
			reason = err.Error()
		}
		switchReasons = append(switchReasons, selection.Account.Guid+":"+reason)
	}
	statusCode := lastOutput.StatusCode
	if statusCode == 0 {
		statusCode = http.StatusBadGateway
	}
	errorType := ""
	latencyMs := time.Since(start).Milliseconds()
	firstTokenMs := int64(0)
	inputTokens := int64(0)
	cachedInputTokens := int64(0)
	outputTokens := int64(0)
	if lastResult != nil {
		errorType = lastResult.ErrorType
		latencyMs = lastResult.LatencyMs
		firstTokenMs = lastResult.FirstTokenMs
		inputTokens = lastResult.Usage.InputTokens
		cachedInputTokens = lastResult.Usage.CachedInputTokens
		outputTokens = lastResult.Usage.OutputTokens
	}
	if lastErr != nil && errorType == "" {
		errorType = classifyError(lastErr)
	}
	serviceTier := firstNonEmpty(logMeta.ServiceTier, platformKey.ServiceTier)
	cost := ModelPricingServiceApp.EstimateCost(
		lastSelection.Model.VendorCode, lastSelection.Model.UpstreamModel, serviceTier,
		inputTokens, cachedInputTokens, outputTokens,
	)
	RequestLogServiceApp.Record(RequestLogInput{
		RequestID:         requestID,
		Method:            logMeta.Method,
		Path:              logMeta.Path,
		PlatformKeyID:     platformKey.Guid,
		PlatformKey:       platformKey.Name,
		KeyPrefix:         platformKey.KeyPrefix,
		AccountGuid:       lastSelection.Account.Guid,
		AccountName:       lastSelection.Account.Name,
		Model:             modelName,
		UpstreamModel:     lastSelection.Model.UpstreamModel,
		ReasoningEffort:   firstNonEmpty(logMeta.ReasoningEffort, platformKey.ReasoningEffort),
		ServiceTier:       serviceTier,
		StatusCode:        statusCode,
		ErrorType:         errorType,
		Switched:          len(switchReasons) > 0,
		SwitchCount:       len(switchReasons),
		SwitchReason:      strings.Join(switchReasons, ";"),
		LatencyMs:         latencyMs,
		FirstTokenMs:      firstTokenMs,
		InputTokens:       inputTokens,
		CachedInputTokens: cachedInputTokens,
		OutputTokens:      outputTokens,
		CostMicrousd:      cost.CostMicrousd,
		PricingMatched:    cost.Matched,
		PricingSource:     cost.SourceKind,
	})
	_ = PlatformKeyServiceApp.FinalizeTokens(platformKey.Guid, reservation, inputTokens+outputTokens, cost.CostMicrousd)
	settledReservation = true
	return lastOutput, lastErr
}

type requestLogMetadata struct {
	Method          string
	Path            string
	Model           string
	ReasoningEffort string
	ServiceTier     string
}

func requestLogMeta(r *http.Request, endpoint string, body []byte) requestLogMetadata {
	meta := requestLogMetadata{
		Method: r.Method,
		Path:   endpoint,
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return meta
	}
	if model, ok := payload["model"].(string); ok {
		meta.Model = strings.TrimSpace(model)
	}
	if reasoning, ok := payload["reasoning"].(map[string]any); ok {
		if effort, ok := reasoning["effort"].(string); ok {
			meta.ReasoningEffort = strings.TrimSpace(effort)
		}
	}
	if meta.ReasoningEffort == "" {
		if effort, ok := payload["reasoning_effort"].(string); ok {
			meta.ReasoningEffort = strings.TrimSpace(effort)
		}
	}
	if serviceTier, ok := payload["service_tier"].(string); ok {
		meta.ServiceTier = strings.TrimSpace(serviceTier)
	}
	return meta
}

func PlatformKeyPrefixFromHeader(header string) string {
	token := strings.TrimSpace(header)
	if strings.HasPrefix(strings.ToLower(token), "bearer ") {
		token = strings.TrimSpace(token[7:])
	}
	if len(token) <= 10 {
		return token
	}
	return token[:10]
}

func applyPlatformKeyRequestOverrides(body []byte, key domains.PlatformKey, modelName string) []byte {
	if key.BoundModel == "" && key.ReasoningEffort == "" && key.ServiceTier == "" {
		return body
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}
	if modelName != "" {
		payload["model"] = modelName
	}
	if key.ReasoningEffort != "" {
		reasoning, _ := payload["reasoning"].(map[string]any)
		if reasoning == nil {
			reasoning = map[string]any{}
		}
		reasoning["effort"] = key.ReasoningEffort
		payload["reasoning"] = reasoning
	}
	if key.ServiceTier != "" {
		payload["service_tier"] = key.ServiceTier
	}
	updated, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return updated
}

func (s ProxyService) callUpstream(r *http.Request, w io.Writer, endpoint string, body []byte, stream bool, selection RouteSelection) (*ProxyResult, ProxyOutput, error) {
	req := ProxyRequest{
		Endpoint: endpoint,
		Model:    selection.Model.UpstreamModel,
		Body:     body,
		Stream:   stream,
	}
	timeout := effectiveUpstreamTimeout(selection.Model.TimeoutSec, Config().RequestTimeout())
	ctx, cancel := contextWithOptionalTimeout(r.Context(), timeout)
	defer cancel()
	var result *ProxyResult
	var err error
	if stream {
		result, err = ProxyAPIClientApp.Stream(ctx, selection.Account, req, w)
	} else {
		result, err = ProxyAPIClientApp.Do(ctx, selection.Account, req)
	}
	if result == nil {
		return nil, ProxyOutput{StatusCode: http.StatusBadGateway}, err
	}
	return result, ProxyOutput{StatusCode: result.StatusCode, Header: result.Header, Body: result.Body}, err
}

// effectiveUpstreamTimeout 计算单次代理请求的总超时。
// 网关总超时为 0 时全局关闭；模型为 0 时继承网关设置。
func effectiveUpstreamTimeout(modelTimeoutSec int, gatewayTimeout time.Duration) time.Duration {
	if gatewayTimeout <= 0 {
		return 0
	}
	if modelTimeoutSec <= 0 {
		return gatewayTimeout
	}
	return time.Duration(modelTimeoutSec) * time.Second
}

func shouldRetry(result *ProxyResult, err error, stream bool) bool {
	if stream && result != nil && result.StreamStarted {
		return false
	}
	errorType := ""
	if result != nil {
		errorType = result.ErrorType
	}
	if errorType == "" && err != nil {
		errorType = classifyError(err)
	}
	switch errorType {
	case domains.ErrorAuthFailed, domains.ErrorRateLimited, domains.ErrorQuotaExhausted, domains.ErrorUpstreamTimeout, domains.ErrorUpstream5xx, domains.ErrorNetwork:
		return true
	default:
		return false
	}
}
