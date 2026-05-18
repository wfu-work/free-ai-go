package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	proxyapi "github.com/free-model/proxy-api-lib"
	"github.com/free-model/proxy-api-lib/auth"
	"github.com/free-model/proxy-api-lib/compat/chatcompletions"
	"github.com/free-model/proxy-api-lib/compat/cliproxyapi"
	"github.com/free-model/proxy-api-lib/compatible"
	proxydomains "github.com/free-model/proxy-api-lib/domains"
)

type ProxyProviderConfig struct {
	Name    string
	BaseURL string
	WireAPI string
}

type ProxyCredential struct {
	Type  string
	Value string
}

type ProxyRequest struct {
	Endpoint string
	Model    string
	Body     []byte
	Stream   bool
}

type ProxyUsage struct {
	InputTokens  int64
	OutputTokens int64
}

type ProxyResult struct {
	StatusCode    int
	Header        http.Header
	Body          []byte
	Usage         ProxyUsage
	ErrorType     string
	FirstTokenMs  int64
	LatencyMs     int64
	StreamStarted bool
}

type ProxyAPIClient interface {
	Do(ctx context.Context, provider ProxyProviderConfig, credential ProxyCredential, req ProxyRequest) (*ProxyResult, error)
	Stream(ctx context.Context, provider ProxyProviderConfig, credential ProxyCredential, req ProxyRequest, w io.Writer) (*ProxyResult, error)
}

type FreeModelProxyAPIClient struct{}

var ProxyAPIClientApp ProxyAPIClient = FreeModelProxyAPIClient{}

func (FreeModelProxyAPIClient) Do(ctx context.Context, provider ProxyProviderConfig, credential ProxyCredential, req ProxyRequest) (*ProxyResult, error) {
	if req.Endpoint == "/v1/embeddings" {
		return rawForward(ctx, provider, credential, req)
	}
	start := time.Now()
	responseReq, err := convertProxyRequest(req)
	if err != nil {
		return nil, err
	}
	responseReq.Model = req.Model
	client := newProxyClient(provider, credential)
	resp, err := client.Responses.Create(ctx, responseReq)
	if err != nil {
		return apiErrorResult(err, time.Since(start).Milliseconds())
	}
	body, err := responseBody(req, resp)
	if err != nil {
		return nil, err
	}
	return &ProxyResult{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       body,
		Usage:      usageFromResponse(resp),
		LatencyMs:  time.Since(start).Milliseconds(),
	}, nil
}

func (FreeModelProxyAPIClient) Stream(ctx context.Context, provider ProxyProviderConfig, credential ProxyCredential, req ProxyRequest, w io.Writer) (*ProxyResult, error) {
	start := time.Now()
	responseReq, err := convertProxyRequest(req)
	if err != nil {
		return nil, err
	}
	responseReq.Model = req.Model
	client := newProxyClient(provider, credential)
	stream, err := client.Responses.Stream(ctx, responseReq)
	if err != nil {
		return apiErrorResult(err, time.Since(start).Milliseconds())
	}
	defer stream.Close()

	result := &ProxyResult{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type":  []string{"text/event-stream"},
			"Cache-Control": []string{"no-cache"},
			"Connection":    []string{"keep-alive"},
		},
	}
	flusher, _ := w.(http.Flusher)
	for stream.Next() {
		event := stream.Event()
		if result.FirstTokenMs == 0 {
			result.FirstTokenMs = time.Since(start).Milliseconds()
		}
		result.StreamStarted = true
		if err := writeStreamEvent(w, req, event); err != nil {
			result.LatencyMs = time.Since(start).Milliseconds()
			result.ErrorType = classifyError(err)
			return result, err
		}
		if flusher != nil {
			flusher.Flush()
		}
		if completed, ok := event.CompletedResponse(); ok {
			result.Usage = usageFromResponse(completed)
		}
	}
	if req.Endpoint == "/v1/chat/completions" && result.StreamStarted {
		if _, err := fmt.Fprint(w, "data: [DONE]\n\n"); err != nil {
			result.LatencyMs = time.Since(start).Milliseconds()
			result.ErrorType = classifyError(err)
			return result, err
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
	result.LatencyMs = time.Since(start).Milliseconds()
	if err := stream.Err(); err != nil {
		result.ErrorType = classifyError(err)
		if !result.StreamStarted {
			errResult, _ := apiErrorResult(err, result.LatencyMs)
			return errResult, nil
		}
		return result, err
	}
	return result, nil
}

func newProxyClient(provider ProxyProviderConfig, credential ProxyCredential) *proxyapi.Client {
	wireAPI := provider.WireAPI
	if wireAPI == "" {
		wireAPI = compatible.WireAPIResponses
	}
	return proxyapi.NewClient(
		proxyapi.WithProvider(compatible.OpenAIResponses(compatible.Config{
			Name:    provider.Name,
			BaseURL: provider.BaseURL,
			WireAPI: wireAPI,
		})),
		proxyapi.WithCredential(proxyCredential(credential)),
	)
}

func proxyCredential(credential ProxyCredential) proxydomains.Credential {
	switch credential.Type {
	case "api_key":
		return auth.APIKey(credential.Value)
	default:
		return auth.BearerToken(credential.Value)
	}
}

func convertProxyRequest(req ProxyRequest) (proxydomains.ResponseRequest, error) {
	switch req.Endpoint {
	case "/v1/chat/completions":
		return chatcompletions.ConvertJSON(req.Body)
	case "/v1/responses":
		return cliproxyapi.ConvertResponsesJSON(req.Body)
	default:
		return proxydomains.ResponseRequest{}, fmt.Errorf("proxy-api-lib does not support endpoint %s", req.Endpoint)
	}
}

func usageFromResponse(resp *proxydomains.Response) ProxyUsage {
	if resp == nil || resp.Usage == nil {
		return ProxyUsage{}
	}
	return ProxyUsage{
		InputTokens:  int64(resp.Usage.InputTokens),
		OutputTokens: int64(resp.Usage.OutputTokens),
	}
}

func responseBody(req ProxyRequest, resp *proxydomains.Response) ([]byte, error) {
	if req.Endpoint == "/v1/chat/completions" {
		return json.Marshal(chatCompletionResponse(req.Model, resp))
	}
	if len(resp.Raw) > 0 {
		return resp.Raw, nil
	}
	return json.Marshal(resp)
}

func chatCompletionResponse(model string, resp *proxydomains.Response) map[string]any {
	id := "chatcmpl"
	if resp != nil && resp.ID != "" {
		id = resp.ID
	}
	message := map[string]any{
		"role":    "assistant",
		"content": "",
	}
	if resp != nil {
		message["content"] = resp.OutputText()
		if calls := resp.ToolCalls(); len(calls) > 0 {
			toolCalls := make([]map[string]any, 0, len(calls))
			for i, call := range calls {
				callID := call.CallID
				if callID == "" {
					callID = call.ID
				}
				toolCalls = append(toolCalls, map[string]any{
					"id":    callID,
					"type":  "function",
					"index": i,
					"function": map[string]any{
						"name":      call.Name,
						"arguments": call.Arguments,
					},
				})
			}
			message["tool_calls"] = toolCalls
			message["content"] = nil
		}
	}
	finishReason := "stop"
	if _, ok := message["tool_calls"]; ok {
		finishReason = "tool_calls"
	}
	return map[string]any{
		"id":      id,
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]any{
			{
				"index":         0,
				"message":       message,
				"finish_reason": finishReason,
			},
		},
		"usage": chatUsage(resp),
	}
}

func chatUsage(resp *proxydomains.Response) map[string]int {
	if resp == nil || resp.Usage == nil {
		return map[string]int{}
	}
	return map[string]int{
		"prompt_tokens":     resp.Usage.InputTokens,
		"completion_tokens": resp.Usage.OutputTokens,
		"total_tokens":      resp.Usage.TotalTokens,
	}
}

func writeStreamEvent(w io.Writer, req ProxyRequest, event proxydomains.StreamEvent) error {
	if req.Endpoint != "/v1/chat/completions" {
		_, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, string(event.Data))
		return err
	}
	delta := event.TextDelta()
	if completed, ok := event.CompletedResponse(); ok {
		finishReason := "stop"
		if completed != nil && len(completed.ToolCalls()) > 0 {
			finishReason = "tool_calls"
		}
		return writeChatCompletionChunk(w, req.Model, map[string]any{}, finishReason)
	}
	if delta == "" {
		return nil
	}
	return writeChatCompletionChunk(w, req.Model, map[string]any{"content": delta}, "")
}

func writeChatCompletionChunk(w io.Writer, model string, delta map[string]any, finishReason any) error {
	if finishReason == "" {
		finishReason = nil
	}
	chunk := map[string]any{
		"id":      "chatcmpl-stream",
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]any{
			{
				"index":         0,
				"delta":         delta,
				"finish_reason": finishReason,
			},
		},
	}
	data, err := json.Marshal(chunk)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", data)
	return err
}

func apiErrorResult(err error, latencyMs int64) (*ProxyResult, error) {
	status := http.StatusBadGateway
	errorType := classifyError(err)
	var apiErr *proxydomains.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode > 0 {
		status = apiErr.StatusCode
	}
	body, marshalErr := json.Marshal(map[string]any{
		"error": map[string]any{
			"message": err.Error(),
			"type":    errorType,
			"code":    errorType,
		},
	})
	if marshalErr != nil {
		return nil, marshalErr
	}
	return &ProxyResult{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       body,
		ErrorType:  errorType,
		LatencyMs:  latencyMs,
	}, nil
}

func rawForward(ctx context.Context, provider ProxyProviderConfig, credential ProxyCredential, req ProxyRequest) (*ProxyResult, error) {
	start := time.Now()
	body, err := rewriteModel(req.Body, req.Model)
	if err != nil {
		return nil, err
	}
	target := strings.TrimRight(provider.BaseURL, "/") + normalizeEndpoint(req.Endpoint, provider.BaseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	authHeader, err := proxyCredential(credential).AuthorizationHeader(ctx)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", authHeader)
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return &ProxyResult{StatusCode: http.StatusBadGateway, ErrorType: classifyError(err), LatencyMs: time.Since(start).Milliseconds()}, nil
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &ProxyResult{
		StatusCode: resp.StatusCode,
		Header:     resp.Header.Clone(),
		Body:       respBody,
		LatencyMs:  time.Since(start).Milliseconds(),
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		result.ErrorType = classifyHTTPStatus(resp.StatusCode, respBody)
	}
	return result, nil
}

func rewriteModel(body []byte, model string) ([]byte, error) {
	if model == "" {
		return body, nil
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	payload["model"] = model
	return json.Marshal(payload)
}

func normalizeEndpoint(endpoint, baseURL string) string {
	if strings.HasSuffix(strings.TrimRight(baseURL, "/"), "/v1") && strings.HasPrefix(endpoint, "/v1/") {
		return strings.TrimPrefix(endpoint, "/v1")
	}
	if strings.HasPrefix(endpoint, "/") {
		return endpoint
	}
	return "/" + endpoint
}

func classifyError(err error) string {
	if err == nil {
		return ""
	}
	var apiErr *proxydomains.APIError
	if errors.As(err, &apiErr) {
		return classifyAPIError(apiErr)
	}
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "no_available_account"):
		return "no_available_account"
	case strings.Contains(text, "model_not_supported") || strings.Contains(text, "record not found"):
		return "model_not_supported"
	case strings.Contains(text, "timeout") || strings.Contains(text, "deadline"):
		return "upstream_timeout"
	case strings.Contains(text, "network") || strings.Contains(text, "connection"):
		return "network_error"
	default:
		return "upstream_5xx"
	}
}

func classifyAPIError(err *proxydomains.APIError) string {
	if err == nil {
		return ""
	}
	text := strings.ToLower(err.Code + " " + err.Type + " " + err.Message)
	switch {
	case err.StatusCode == http.StatusUnauthorized || err.StatusCode == http.StatusForbidden:
		return "auth_failed"
	case err.StatusCode == http.StatusTooManyRequests:
		return "rate_limited"
	case strings.Contains(text, "quota") || strings.Contains(text, "insufficient"):
		return "quota_exhausted"
	case err.StatusCode == http.StatusRequestTimeout || err.StatusCode == http.StatusGatewayTimeout:
		return "upstream_timeout"
	case err.StatusCode >= 500:
		return "upstream_5xx"
	default:
		return "network_error"
	}
}

func classifyHTTPStatus(status int, body []byte) string {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return "auth_failed"
	case status == http.StatusTooManyRequests:
		return "rate_limited"
	case status == http.StatusRequestTimeout || status == http.StatusGatewayTimeout:
		return "upstream_timeout"
	case status >= 500:
		return "upstream_5xx"
	}
	text := strings.ToLower(string(body))
	if strings.Contains(text, "quota") || strings.Contains(text, "insufficient") {
		return "quota_exhausted"
	}
	return "network_error"
}
