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

	proxyapi "github.com/wfu-work/proxy-api-lib"
	"github.com/wfu-work/proxy-api-lib/auth"
	"github.com/wfu-work/proxy-api-lib/codec/chatcompletions"
	responsescodec "github.com/wfu-work/proxy-api-lib/codec/responses"
	"github.com/wfu-work/proxy-api-lib/openai"
)

type ProxyProviderConfig struct {
	Name string
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

type ProxyAPIClientImpl struct{}

var ProxyAPIClientApp ProxyAPIClient = ProxyAPIClientImpl{}

func (ProxyAPIClientImpl) Do(ctx context.Context, provider ProxyProviderConfig, credential ProxyCredential, req ProxyRequest) (*ProxyResult, error) {
	if err := validateOfficialProvider(provider); err != nil {
		return nil, err
	}
	if req.Endpoint == "/v1/embeddings" {
		return doEmbedding(ctx, credential, req)
	}
	start := time.Now()
	responseReq, err := convertProxyRequest(req)
	if err != nil {
		return nil, err
	}
	responseReq.Model = req.Model
	client, err := newProxyClient(credential)
	if err != nil {
		return nil, err
	}
	resp, err := client.Responses.Create(ctx, responseReq)
	if err != nil {
		return apiErrorResult(err, time.Since(start).Milliseconds())
	}
	body, err := responseBody(req, resp)
	if err != nil {
		return nil, err
	}
	return &ProxyResult{
		StatusCode:   http.StatusOK,
		Header:       http.Header{"Content-Type": []string{"application/json"}},
		Body:         body,
		Usage:        usageFromResponse(resp),
		FirstTokenMs: time.Since(start).Milliseconds(),
		LatencyMs:    time.Since(start).Milliseconds(),
	}, nil
}

func (ProxyAPIClientImpl) Stream(ctx context.Context, provider ProxyProviderConfig, credential ProxyCredential, req ProxyRequest, w io.Writer) (*ProxyResult, error) {
	if err := validateOfficialProvider(provider); err != nil {
		return nil, err
	}
	start := time.Now()
	responseReq, err := convertProxyRequest(req)
	if err != nil {
		return nil, err
	}
	responseReq.Model = req.Model
	client, err := newProxyClient(credential)
	if err != nil {
		return nil, err
	}
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
	streamErr := stream.Err()
	if req.Endpoint == "/v1/chat/completions" && result.StreamStarted && streamErr == nil {
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
	if streamErr != nil {
		result.ErrorType = classifyError(streamErr)
		if !result.StreamStarted {
			errResult, _ := apiErrorResult(streamErr, result.LatencyMs)
			return errResult, nil
		}
		return result, streamErr
	}
	return result, nil
}

func newProxyClient(credential ProxyCredential) (*proxyapi.Client, error) {
	httpClient, err := UpstreamHTTPClient()
	if err != nil {
		return nil, err
	}
	return proxyapi.NewClient(
		proxyapi.WithHTTPClient(httpClient),
		proxyapi.WithCredential(proxyCredential(credential)),
	), nil
}

func validateOfficialProvider(provider ProxyProviderConfig) error {
	name := strings.ToLower(strings.TrimSpace(provider.Name))
	if name != "" && name != "openai" {
		return fmt.Errorf("unsupported provider %q: only official OpenAI accounts are enabled", provider.Name)
	}
	return nil
}

func proxyCredential(credential ProxyCredential) openai.Credential {
	switch credential.Type {
	case "api_key":
		return auth.APIKey(credential.Value)
	case "login_callback":
		return auth.BearerToken(loginCallbackAccessToken(credential.Value))
	default:
		return auth.BearerToken(credential.Value)
	}
}

func convertProxyRequest(req ProxyRequest) (openai.ResponseRequest, error) {
	switch req.Endpoint {
	case "/v1/chat/completions":
		return chatcompletions.Decode(req.Body)
	case "/v1/responses":
		return responsescodec.Decode(req.Body)
	default:
		return openai.ResponseRequest{}, fmt.Errorf("proxy-api-lib does not support endpoint %s", req.Endpoint)
	}
}

func usageFromResponse(resp *openai.Response) ProxyUsage {
	if resp == nil || resp.Usage == nil {
		return ProxyUsage{}
	}
	return ProxyUsage{
		InputTokens:  int64(resp.Usage.InputTokens),
		OutputTokens: int64(resp.Usage.OutputTokens),
	}
}

func responseBody(req ProxyRequest, resp *openai.Response) ([]byte, error) {
	if req.Endpoint == "/v1/chat/completions" {
		return json.Marshal(chatCompletionResponse(req.Model, resp))
	}
	if len(resp.Raw) > 0 {
		return resp.Raw, nil
	}
	return json.Marshal(resp)
}

func chatCompletionResponse(model string, resp *openai.Response) map[string]any {
	return chatcompletions.Response(model, resp)
}

func writeStreamEvent(w io.Writer, req ProxyRequest, event openai.StreamEvent) error {
	if req.Endpoint != "/v1/chat/completions" {
		_, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, string(event.Data))
		return err
	}
	if event.Type == "response.created" {
		return writeChatCompletionChunk(w, req.Model, map[string]any{"role": "assistant"}, "")
	}
	if completed, ok := event.CompletedResponse(); ok {
		finishReason := "stop"
		if completed != nil && len(completed.ToolCalls()) > 0 {
			finishReason = "tool_calls"
		}
		return writeChatCompletionChunk(w, req.Model, map[string]any{}, finishReason)
	}
	if added, ok := event.OutputItemAdded(); ok && added.Item.Type == "function_call" {
		callID := added.Item.CallID
		if callID == "" {
			callID = added.Item.ID
		}
		toolCall := map[string]any{
			"index": added.OutputIndex,
			"id":    callID,
			"type":  "function",
			"function": map[string]any{
				"name":      added.Item.Name,
				"arguments": added.Item.Arguments,
			},
		}
		return writeChatCompletionChunk(w, req.Model, map[string]any{"tool_calls": []map[string]any{toolCall}}, "")
	}
	if arguments, ok := event.FunctionCallArgumentsDelta(); ok {
		toolCall := map[string]any{
			"index": arguments.OutputIndex,
			"function": map[string]any{
				"arguments": arguments.Delta,
			},
		}
		return writeChatCompletionChunk(w, req.Model, map[string]any{"tool_calls": []map[string]any{toolCall}}, "")
	}
	delta := event.TextDelta()
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
	var apiErr *openai.APIError
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
		StatusCode:   status,
		Header:       http.Header{"Content-Type": []string{"application/json"}},
		Body:         body,
		ErrorType:    errorType,
		FirstTokenMs: latencyMs,
		LatencyMs:    latencyMs,
	}, nil
}

func doEmbedding(ctx context.Context, credential ProxyCredential, req ProxyRequest) (*ProxyResult, error) {
	start := time.Now()
	var payload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(req.Body))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil, err
	}
	embeddingReq := openai.EmbeddingRequest{
		Model: req.Model,
		Input: payload["input"],
		Extra: map[string]any{},
	}
	if embeddingReq.Model == "" {
		embeddingReq.Model, _ = payload["model"].(string)
	}
	if value, ok := payload["encoding_format"].(string); ok {
		embeddingReq.EncodingFormat = value
	}
	if value, ok := payload["user"].(string); ok {
		embeddingReq.User = value
	}
	if value, ok := integerValue(payload["dimensions"]); ok {
		embeddingReq.Dimensions = &value
	}
	for key, value := range payload {
		switch key {
		case "model", "input", "encoding_format", "dimensions", "user":
		default:
			embeddingReq.Extra[key] = value
		}
	}
	if len(embeddingReq.Extra) == 0 {
		embeddingReq.Extra = nil
	}
	client, err := newProxyClient(credential)
	if err != nil {
		return nil, err
	}
	resp, err := client.Embeddings.Create(ctx, embeddingReq)
	if err != nil {
		return apiErrorResult(err, time.Since(start).Milliseconds())
	}
	body := resp.Raw
	if len(body) == 0 {
		body, err = json.Marshal(resp)
		if err != nil {
			return nil, err
		}
	}
	latencyMs := time.Since(start).Milliseconds()
	return &ProxyResult{
		StatusCode:   http.StatusOK,
		Header:       http.Header{"Content-Type": []string{"application/json"}},
		Body:         body,
		Usage:        ProxyUsage{InputTokens: int64(resp.Usage.PromptTokens)},
		FirstTokenMs: latencyMs,
		LatencyMs:    time.Since(start).Milliseconds(),
	}, nil
}

func integerValue(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case float64:
		return int(typed), true
	case json.Number:
		number, err := typed.Int64()
		return int(number), err == nil
	default:
		return 0, false
	}
}

func classifyError(err error) string {
	if err == nil {
		return ""
	}
	var apiErr *openai.APIError
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

func classifyAPIError(err *openai.APIError) string {
	if err == nil {
		return ""
	}
	text := strings.ToLower(err.Code + " " + err.Type + " " + err.Message)
	switch {
	case strings.Contains(text, "quota") || strings.Contains(text, "insufficient"):
		return "quota_exhausted"
	case err.StatusCode == http.StatusUnauthorized || err.StatusCode == http.StatusForbidden:
		return "auth_failed"
	case err.StatusCode == http.StatusTooManyRequests:
		return "rate_limited"
	case err.StatusCode == http.StatusRequestTimeout || err.StatusCode == http.StatusGatewayTimeout:
		return "upstream_timeout"
	case err.StatusCode >= 500:
		return "upstream_5xx"
	default:
		return "network_error"
	}
}

func classifyHTTPStatus(status int, body []byte) string {
	text := strings.ToLower(string(body))
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return "auth_failed"
	case strings.Contains(text, "quota") || strings.Contains(text, "insufficient"):
		return "quota_exhausted"
	case status == http.StatusTooManyRequests:
		return "rate_limited"
	case status == http.StatusRequestTimeout || status == http.StatusGatewayTimeout:
		return "upstream_timeout"
	case status >= 500:
		return "upstream_5xx"
	}
	return "network_error"
}

func loginCallbackAccessToken(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	var payload map[string]string
	if err := json.Unmarshal([]byte(value), &payload); err == nil {
		if token := strings.TrimSpace(payload["api_key_access_token"]); token != "" {
			return token
		}
		if token := strings.TrimSpace(payload["apiKeyAccessToken"]); token != "" {
			return token
		}
		if token := strings.TrimSpace(payload["api_key_token"]); token != "" {
			return token
		}
		if token := strings.TrimSpace(payload["access_token"]); token != "" {
			return token
		}
		if token := strings.TrimSpace(payload["token"]); token != "" {
			return token
		}
	}
	return value
}
