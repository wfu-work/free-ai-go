package services

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"freeai/domains"
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
	modelName := r.URL.Query().Get("model")
	if modelName == "" {
		modelName = r.Header.Get("X-FreeAi-Model")
	}
	if modelName == "" {
		modelName = r.Header.Get("X-FreeModel-Model")
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
			Model:      modelName,
			StatusCode: status,
			ErrorType:  errorType,
			LatencyMs:  time.Since(start).Milliseconds(),
		})
		return ProxyOutput{StatusCode: status}, err
	}
	if platformKey.BoundModel != "" {
		modelName = platformKey.BoundModel
	}
	if modelName == "" {
		RequestLogServiceApp.Record(RequestLogInput{
			PlatformKeyID: platformKey.Guid,
			StatusCode:    http.StatusBadRequest,
			ErrorType:     domains.ErrorModelNotSupported,
			LatencyMs:     time.Since(start).Milliseconds(),
		})
		return ProxyOutput{StatusCode: http.StatusBadRequest}, errors.New("model is required")
	}
	if !PlatformKeyServiceApp.ModelAllowed(platformKey, modelName) {
		if mapping, findErr := ModelServiceApp.Find(modelName); findErr != nil || !PlatformKeyServiceApp.ModelMappingAllowed(platformKey, mapping) {
			RequestLogServiceApp.Record(RequestLogInput{
				PlatformKeyID: platformKey.Guid,
				Model:         modelName,
				StatusCode:    http.StatusForbidden,
				ErrorType:     domains.ErrorModelNotSupported,
				LatencyMs:     time.Since(start).Milliseconds(),
			})
			return ProxyOutput{StatusCode: http.StatusForbidden}, errors.New(domains.ErrorModelNotSupported)
		}
	}
	body = applyPlatformKeyRequestOverrides(body, platformKey, modelName)

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
		if stream && !selection.Model.Stream {
			lastErr = errors.New(domains.ErrorModelNotSupported + ": stream is not enabled for model")
			lastOutput = ProxyOutput{StatusCode: http.StatusBadRequest}
			break
		}
		excluded[selection.Account.Guid] = true
		result, output, err := s.callUpstream(r, w, endpoint, body, stream, selection)
		lastResult = result
		lastOutput = output
		lastErr = err
		if result != nil && result.ErrorType != "" {
			QuotaServiceApp.ApplyError(selection.Account.Guid, result.ErrorType)
		}
		if err == nil && (result == nil || result.ErrorType == "") {
			if result != nil {
				QuotaServiceApp.ApplyUsage(selection.Account.Guid, result.Usage.InputTokens, result.Usage.OutputTokens)
			}
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
	outputTokens := int64(0)
	if lastResult != nil {
		errorType = lastResult.ErrorType
		latencyMs = lastResult.LatencyMs
		firstTokenMs = lastResult.FirstTokenMs
		inputTokens = lastResult.Usage.InputTokens
		outputTokens = lastResult.Usage.OutputTokens
	}
	if lastErr != nil && errorType == "" {
		errorType = classifyError(lastErr)
	}
	RequestLogServiceApp.Record(RequestLogInput{
		PlatformKeyID: platformKey.Guid,
		AccountGuid:   lastSelection.Account.Guid,
		Model:         modelName,
		UpstreamModel: lastSelection.Model.UpstreamModel,
		Provider:      lastSelection.Model.Provider,
		StatusCode:    statusCode,
		ErrorType:     errorType,
		Switched:      len(switchReasons) > 0,
		SwitchCount:   len(switchReasons),
		SwitchReason:  strings.Join(switchReasons, ";"),
		LatencyMs:     latencyMs,
		FirstTokenMs:  firstTokenMs,
		InputTokens:   inputTokens,
		OutputTokens:  outputTokens,
	})
	return lastOutput, lastErr
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
	secret, err := AccountServiceApp.DecryptSecret(selection.Account)
	if err != nil {
		return nil, ProxyOutput{StatusCode: http.StatusInternalServerError}, err
	}
	req := ProxyRequest{
		Endpoint: endpoint,
		Model:    selection.Model.UpstreamModel,
		Body:     body,
		Stream:   stream,
	}
	provider := ProxyProviderConfig{
		Name:    selection.Model.Provider,
		BaseURL: accountBaseURL(selection.Account),
		WireAPI: "responses",
	}
	credential := ProxyCredential{Type: selection.Account.AuthType, Value: secret}
	timeout := time.Duration(selection.Model.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = Config().RequestTimeout()
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	var result *ProxyResult
	if stream {
		result, err = ProxyAPIClientApp.Stream(ctx, provider, credential, req, w)
	} else {
		result, err = ProxyAPIClientApp.Do(ctx, provider, credential, req)
	}
	if result == nil {
		return nil, ProxyOutput{StatusCode: http.StatusBadGateway}, err
	}
	return result, ProxyOutput{StatusCode: result.StatusCode, Header: result.Header, Body: result.Body}, err
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
