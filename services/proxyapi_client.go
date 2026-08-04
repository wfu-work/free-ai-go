package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/wfu-work/free-ai-go/domains"
	"github.com/wfu-work/proxy-api-lib/chatgpt"
	"github.com/wfu-work/proxy-api-lib/codec/chatcompletions"
	responsescodec "github.com/wfu-work/proxy-api-lib/codec/responses"
	"github.com/wfu-work/proxy-api-lib/openai"
)

type ProxyRequest struct {
	Endpoint string
	Model    string
	Body     []byte
	Stream   bool
}

type ProxyUsage struct {
	InputTokens       int64
	CachedInputTokens int64
	OutputTokens      int64
}

type ProxyResult struct {
	StatusCode       int
	Header           http.Header
	Body             []byte
	Usage            ProxyUsage
	ErrorType        string
	PreparationMs    int64
	DNSMs            int64
	ConnectMs        int64
	TLSHandshakeMs   int64
	UpstreamHeaderMs int64
	FirstEventMs     int64
	FirstTokenMs     int64
	LatencyMs        int64
	ConnectionReused bool
	ConnectionTraced bool
	StreamStarted    bool
}

type ProxyAPIClient interface {
	Do(ctx context.Context, account domains.Account, req ProxyRequest) (*ProxyResult, error)
	Stream(ctx context.Context, account domains.Account, req ProxyRequest, w io.Writer) (*ProxyResult, error)
}

type ProxyAPIClientImpl struct{}

var ProxyAPIClientApp ProxyAPIClient = ProxyAPIClientImpl{}

func (ProxyAPIClientImpl) Do(ctx context.Context, account domains.Account, req ProxyRequest) (*ProxyResult, error) {
	start := time.Now()
	responseReq, err := convertProxyRequest(req)
	if err != nil {
		return nil, err
	}
	responseReq.Model = req.Model
	file, err := AccountServiceApp.ActiveAccountFile(ctx, account, false)
	if err != nil {
		return apiErrorResult(err, time.Since(start).Milliseconds())
	}
	client, err := chatGPTClient(file)
	if err != nil {
		return nil, err
	}
	preparationMs := time.Since(start).Milliseconds()
	trace := newUpstreamRequestTrace()
	result, err := client.Codex.Create(trace.context(ctx), accountRouteID(account, file), responseReq)
	if isChatGPTUnauthorized(err) {
		if file, refreshErr := AccountServiceApp.ActiveAccountFile(ctx, account, true); refreshErr == nil {
			client, _ = chatGPTClient(file)
			preparationMs = time.Since(start).Milliseconds()
			trace = newUpstreamRequestTrace()
			result, err = client.Codex.Create(trace.context(ctx), accountRouteID(account, file), responseReq)
		}
	}
	if err != nil {
		proxyResult, proxyErr := apiErrorResult(err, time.Since(start).Milliseconds())
		applyUpstreamTiming(proxyResult, preparationMs, trace)
		return proxyResult, proxyErr
	}
	body, err := responseBody(req, result.Response)
	if err != nil {
		return nil, err
	}
	latency := time.Since(start).Milliseconds()
	proxyResult := &ProxyResult{
		StatusCode: result.StatusCode, Header: result.Header.Clone(), Body: body,
		Usage: usageFromResponse(result.Response), FirstTokenMs: latency, LatencyMs: latency,
	}
	applyUpstreamTiming(proxyResult, preparationMs, trace)
	return proxyResult, nil
}

func (ProxyAPIClientImpl) Stream(ctx context.Context, account domains.Account, req ProxyRequest, w io.Writer) (*ProxyResult, error) {
	start := time.Now()
	responseReq, err := convertProxyRequest(req)
	if err != nil {
		return nil, err
	}
	responseReq.Model = req.Model
	file, err := AccountServiceApp.ActiveAccountFile(ctx, account, false)
	if err != nil {
		return apiErrorResult(err, time.Since(start).Milliseconds())
	}
	client, err := chatGPTClient(file)
	if err != nil {
		return nil, err
	}
	preparationMs := time.Since(start).Milliseconds()
	trace := newUpstreamRequestTrace()
	upstream, err := client.Codex.Stream(trace.context(ctx), accountRouteID(account, file), responseReq)
	if isChatGPTUnauthorized(err) {
		if file, refreshErr := AccountServiceApp.ActiveAccountFile(ctx, account, true); refreshErr == nil {
			client, _ = chatGPTClient(file)
			preparationMs = time.Since(start).Milliseconds()
			trace = newUpstreamRequestTrace()
			upstream, err = client.Codex.Stream(trace.context(ctx), accountRouteID(account, file), responseReq)
		}
	}
	if err != nil {
		proxyResult, proxyErr := apiErrorResult(err, time.Since(start).Milliseconds())
		applyUpstreamTiming(proxyResult, preparationMs, trace)
		return proxyResult, proxyErr
	}
	stream := upstream.Stream
	defer stream.Close()
	result := &ProxyResult{StatusCode: upstream.StatusCode, Header: upstream.Header.Clone()}
	applyUpstreamTiming(result, preparationMs, trace)
	if result.Header == nil {
		result.Header = make(http.Header)
	}
	result.Header.Set("Content-Type", "text/event-stream")
	result.Header.Set("Cache-Control", "no-cache")
	result.Header.Set("Connection", "keep-alive")
	flusher, _ := w.(http.Flusher)
	for stream.Next() {
		event := stream.Event()
		elapsedMs := time.Since(start).Milliseconds()
		if result.FirstEventMs == 0 {
			result.FirstEventMs = elapsedMs
		}
		if result.FirstTokenMs == 0 && isContentStreamEvent(event) {
			result.FirstTokenMs = elapsedMs
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
			applyUpstreamTiming(errResult, preparationMs, trace)
			return errResult, nil
		}
		return result, streamErr
	}
	return result, nil
}

func applyUpstreamTiming(result *ProxyResult, preparationMs int64, trace *upstreamRequestTrace) {
	if result == nil {
		return
	}
	timing := trace.snapshot()
	result.PreparationMs = preparationMs
	result.DNSMs = timing.DNSMs
	result.ConnectMs = timing.ConnectMs
	result.TLSHandshakeMs = timing.TLSHandshakeMs
	result.UpstreamHeaderMs = timing.UpstreamHeaderMs
	result.ConnectionReused = timing.ConnectionReused
	result.ConnectionTraced = timing.GotConnection
}

func isContentStreamEvent(event openai.StreamEvent) bool {
	switch event.Type {
	case openai.EventResponseOutputTextDelta, openai.EventResponseFunctionArgumentsDelta:
		return true
	default:
		return strings.HasPrefix(event.Type, "response.reasoning") && strings.HasSuffix(event.Type, ".delta")
	}
}

func convertProxyRequest(req ProxyRequest) (openai.ResponseRequest, error) {
	switch req.Endpoint {
	case "/v1/chat/completions":
		return chatcompletions.Decode(req.Body)
	case "/v1/responses":
		return responsescodec.Decode(req.Body)
	default:
		return openai.ResponseRequest{}, fmt.Errorf("Codex account pool does not support endpoint %s", req.Endpoint)
	}
}

func usageFromResponse(resp *openai.Response) ProxyUsage {
	if resp == nil || resp.Usage == nil {
		return ProxyUsage{}
	}
	usage := ProxyUsage{InputTokens: int64(resp.Usage.InputTokens), OutputTokens: int64(resp.Usage.OutputTokens)}
	if resp.Usage.InputTokensDetails != nil {
		usage.CachedInputTokens = int64(resp.Usage.InputTokensDetails.CachedTokens)
	}
	return usage
}

func responseBody(req ProxyRequest, resp *openai.Response) ([]byte, error) {
	if req.Endpoint == "/v1/chat/completions" {
		return json.Marshal(chatcompletions.Response(req.Model, resp))
	}
	if resp != nil && len(resp.Raw) > 0 {
		return resp.Raw, nil
	}
	return json.Marshal(resp)
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
		callID := firstNonEmpty(added.Item.CallID, added.Item.ID)
		toolCall := map[string]any{"index": added.OutputIndex, "id": callID, "type": "function", "function": map[string]any{"name": added.Item.Name, "arguments": added.Item.Arguments}}
		return writeChatCompletionChunk(w, req.Model, map[string]any{"tool_calls": []map[string]any{toolCall}}, "")
	}
	if arguments, ok := event.FunctionCallArgumentsDelta(); ok {
		toolCall := map[string]any{"index": arguments.OutputIndex, "function": map[string]any{"arguments": arguments.Delta}}
		return writeChatCompletionChunk(w, req.Model, map[string]any{"tool_calls": []map[string]any{toolCall}}, "")
	}
	if delta := event.TextDelta(); delta != "" {
		return writeChatCompletionChunk(w, req.Model, map[string]any{"content": delta}, "")
	}
	return nil
}

func writeChatCompletionChunk(w io.Writer, model string, delta map[string]any, finishReason any) error {
	if finishReason == "" {
		finishReason = nil
	}
	chunk := map[string]any{
		"id": "chatcmpl-stream", "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": model,
		"choices": []map[string]any{{"index": 0, "delta": delta, "finish_reason": finishReason}},
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
	var openAIErr *openai.APIError
	var chatGPTErr *chatgpt.APIError
	if errors.As(err, &openAIErr) && openAIErr.StatusCode > 0 {
		status = openAIErr.StatusCode
	} else if errors.As(err, &chatGPTErr) && chatGPTErr.StatusCode > 0 {
		status = chatGPTErr.StatusCode
	}
	body, marshalErr := json.Marshal(map[string]any{"error": map[string]any{"message": err.Error(), "type": errorType, "code": errorType}})
	if marshalErr != nil {
		return nil, marshalErr
	}
	return &ProxyResult{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: body, ErrorType: errorType, LatencyMs: latencyMs}, nil
}

func classifyError(err error) string {
	if err == nil {
		return ""
	}
	var openAIErr *openai.APIError
	if errors.As(err, &openAIErr) {
		return classifyStatusError(openAIErr.StatusCode, openAIErr.Code+" "+openAIErr.Type+" "+openAIErr.Message)
	}
	var chatGPTErr *chatgpt.APIError
	if errors.As(err, &chatGPTErr) {
		return classifyStatusError(chatGPTErr.StatusCode, chatGPTErr.Code+" "+chatGPTErr.Type+" "+chatGPTErr.Message)
	}
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "no_available_account"):
		return domains.ErrorNoAvailableAccount
	case strings.Contains(text, "model_not_supported") || strings.Contains(text, "record not found"):
		return domains.ErrorModelNotSupported
	case strings.Contains(text, "oauth") || strings.Contains(text, "refresh_token") || strings.Contains(text, "refresh token"):
		return domains.ErrorAuthFailed
	case strings.Contains(text, "timeout") || strings.Contains(text, "deadline"):
		return domains.ErrorUpstreamTimeout
	case strings.Contains(text, "network") || strings.Contains(text, "connection"):
		return domains.ErrorNetwork
	default:
		return domains.ErrorUpstream5xx
	}
}

func classifyStatusError(status int, message string) string {
	text := strings.ToLower(message)
	switch {
	case strings.Contains(text, "quota") || strings.Contains(text, "insufficient"):
		return domains.ErrorQuotaExhausted
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return domains.ErrorAuthFailed
	case status == http.StatusTooManyRequests:
		return domains.ErrorRateLimited
	case status == http.StatusRequestTimeout || status == http.StatusGatewayTimeout:
		return domains.ErrorUpstreamTimeout
	case status >= 500:
		return domains.ErrorUpstream5xx
	default:
		return domains.ErrorNetwork
	}
}
