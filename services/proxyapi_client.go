package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
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
	Endpoint             string
	Model                string
	Body                 []byte
	Stream               bool
	FirstResponseTimeout time.Duration
	StreamIdleTimeout    time.Duration
	ContextCompaction    bool
	CompactionThreshold  int64
}

type ProxyUsage struct {
	InputTokens       int64
	CachedInputTokens int64
	OutputTokens      int64
}

type ProxyResult struct {
	StatusCode         int
	Header             http.Header
	Body               []byte
	BufferedStreamBody []byte
	Usage              ProxyUsage
	ErrorType          string
	ErrorSummary       string
	DiagnosticType     string
	DiagnosticSummary  string
	PreparationMs      int64
	DNSMs              int64
	ConnectMs          int64
	TLSHandshakeMs     int64
	UpstreamHeaderMs   int64
	FirstEventMs       int64
	FirstTokenMs       int64
	LatencyMs          int64
	ConnectionReused   bool
	ConnectionTraced   bool
	StreamStarted      bool
}

type ProxyAPIClient interface {
	Do(ctx context.Context, account domains.Account, req ProxyRequest) (*ProxyResult, error)
	Stream(ctx context.Context, account domains.Account, req ProxyRequest, w io.Writer) (*ProxyResult, error)
}

type ProxyAPIClientImpl struct{}

var ProxyAPIClientApp ProxyAPIClient = ProxyAPIClientImpl{}

func (ProxyAPIClientImpl) Do(ctx context.Context, account domains.Account, req ProxyRequest) (*ProxyResult, error) {
	if req.Endpoint == "/v1/images/generations" {
		if account.ProductCode != domains.ProductOpenAIImages || account.CredentialType != domains.CredentialAPIKey {
			return unsupportedEndpointResult(req.Endpoint, req.Model)
		}
		req.Body = replaceProxyModel(req.Body, req.Model)
		return OpenAIImageServiceApp.Generate(ctx, account, req.Body)
	}
	if account.ProductCode == domains.ProductOpenAIImages {
		return unsupportedEndpointResult(req.Endpoint, req.Model)
	}
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
	applyContextCompactionDiagnostic(proxyResult, responseContainsCompaction(result.Response), req.CompactionThreshold)
	applyUpstreamTiming(proxyResult, preparationMs, trace)
	return proxyResult, nil
}

func (ProxyAPIClientImpl) Stream(ctx context.Context, account domains.Account, req ProxyRequest, w io.Writer) (*ProxyResult, error) {
	if account.ProductCode == domains.ProductOpenAIImages || req.Endpoint == "/v1/images/generations" {
		return unsupportedEndpointResult(req.Endpoint, req.Model)
	}
	start := time.Now()
	streamCtx, streamTimeouts := newStreamTimeoutContext(ctx, req.FirstResponseTimeout)
	defer func() {
		streamTimeouts.stop()
		streamTimeouts.cancel(context.Canceled)
	}()
	responseReq, err := convertProxyRequest(req)
	if err != nil {
		return nil, err
	}
	responseReq.Model = req.Model
	file, err := AccountServiceApp.ActiveAccountFile(streamCtx, account, false)
	if err != nil {
		err = streamContextError(streamCtx, ctx, err)
		return apiErrorResult(err, time.Since(start).Milliseconds())
	}
	client, err := chatGPTClient(file)
	if err != nil {
		return nil, err
	}
	preparationMs := time.Since(start).Milliseconds()
	trace := newUpstreamRequestTrace()
	upstream, err := client.Codex.Stream(trace.context(streamCtx), accountRouteID(account, file), responseReq)
	if isChatGPTUnauthorized(err) {
		if file, refreshErr := AccountServiceApp.ActiveAccountFile(streamCtx, account, true); refreshErr == nil {
			client, _ = chatGPTClient(file)
			preparationMs = time.Since(start).Milliseconds()
			trace = newUpstreamRequestTrace()
			upstream, err = client.Codex.Stream(trace.context(streamCtx), accountRouteID(account, file), responseReq)
		}
	}
	if err != nil {
		err = streamContextError(streamCtx, ctx, err)
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
	terminalEventType := ""
	responseCompleted := false
	for stream.Next() {
		event := stream.Event()
		// 首个 SSE 到达后，固定的首响应时限即告结束；此后只在连续
		// 没有新事件超过空闲时限时取消上游读取。
		streamTimeouts.noteEvent(req.StreamIdleTimeout)
		applyContextCompactionDiagnostic(result, streamEventContainsCompaction(event), req.CompactionThreshold)
		if streamEventErrorType(event.Type) != "" {
			terminalEventType = event.Type
		}
		elapsedMs := time.Since(start).Milliseconds()
		if result.FirstEventMs == 0 {
			result.FirstEventMs = elapsedMs
		}
		if result.FirstTokenMs == 0 && isContentStreamEvent(event) {
			result.FirstTokenMs = elapsedMs
		}
		// 上游可能用 HTTP 200 建立 SSE，随后把过载作为第一个 error 事件。
		// 此时下游还未看到任何字节，可以安全吞掉该事件并交给路由层切换账号。
		if !result.StreamStarted && isRetryableInitialOverloadEvent(event) {
			result.LatencyMs = elapsedMs
			result.ErrorType = domains.ErrorUpstreamFailed
			result.ErrorSummary = initialStreamErrorSummary(event)
			result.BufferedStreamBody = initialStreamErrorBody(req, event)
			return result, nil
		}
		result.StreamStarted = true
		if err := writeStreamEvent(w, req, event); err != nil {
			result.LatencyMs = time.Since(start).Milliseconds()
			result.ErrorType = classifyDownstreamWriteError(err)
			result.ErrorSummary = proxyErrorSummary(err)
			return result, err
		}
		if flusher != nil {
			flusher.Flush()
		}
		if completed, ok := event.CompletedResponse(); ok {
			result.Usage = usageFromResponse(completed)
			responseCompleted = true
		}
	}
	streamTimeouts.stop()
	streamErr := stream.Err()
	if streamErr != nil || streamCtx.Err() != nil {
		streamErr = streamContextError(streamCtx, ctx, streamErr)
	}
	streamErr = applyPostCompletionStreamError(result, responseCompleted, streamErr)
	if req.Endpoint == "/v1/chat/completions" && result.StreamStarted && streamErr == nil {
		if _, err := fmt.Fprint(w, "data: [DONE]\n\n"); err != nil {
			if writeErr := applyStreamDoneWriteError(result, responseCompleted, err); writeErr != nil {
				return result, writeErr
			}
		} else if flusher != nil {
			flusher.Flush()
		}
	}
	result.LatencyMs = time.Since(start).Milliseconds()
	if streamErr != nil {
		result.ErrorType = streamEventErrorType(terminalEventType)
		if result.ErrorType == "" {
			result.ErrorType = classifyError(streamErr)
		}
		result.ErrorSummary = streamErrorSummary(terminalEventType, streamErr)
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
	var responseReq openai.ResponseRequest
	var err error
	switch req.Endpoint {
	case "/v1/chat/completions":
		responseReq, err = chatcompletions.Decode(req.Body)
	case "/v1/responses":
		responseReq, err = responsescodec.Decode(req.Body)
	default:
		return openai.ResponseRequest{}, fmt.Errorf("Codex account pool does not support endpoint %s", req.Endpoint)
	}
	if err != nil {
		return openai.ResponseRequest{}, err
	}
	// 服务端压缩会在响应中返回必须原样携带到下一轮的加密 compaction
	// item，因此只对能够透传 Responses 输入/输出项的端点启用。
	if req.Endpoint == "/v1/responses" && req.ContextCompaction {
		if responseReq.Extra == nil {
			responseReq.Extra = map[string]any{}
		}
		threshold := normalizeContextCompactionThreshold(req.CompactionThreshold)
		responseReq.Extra["context_management"] = []map[string]any{{
			"type":              "compaction",
			"compact_threshold": threshold,
		}}
	}
	return responseReq, nil
}

func responseContainsCompaction(response *openai.Response) bool {
	if response == nil {
		return false
	}
	for _, item := range response.Output {
		if item.Type == "compaction" {
			return true
		}
	}
	return false
}

func streamEventContainsCompaction(event openai.StreamEvent) bool {
	if strings.Contains(strings.ToLower(event.Type), "compaction") {
		return true
	}
	if added, ok := event.OutputItemAdded(); ok {
		return added.Item.Type == "compaction"
	}
	return false
}

func applyContextCompactionDiagnostic(result *ProxyResult, compacted bool, threshold int64) {
	if result == nil || !compacted {
		return
	}
	result.DiagnosticType = domains.DiagnosticContextCompacted
	result.DiagnosticSummary = fmt.Sprintf("server-side context compaction triggered at %d input tokens", threshold)
}

func replaceProxyModel(body []byte, model string) []byte {
	if strings.TrimSpace(model) == "" {
		return body
	}
	var payload map[string]any
	if json.Unmarshal(body, &payload) != nil {
		return body
	}
	payload["model"] = strings.TrimSpace(model)
	updated, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return updated
}

func unsupportedEndpointResult(endpoint, model string) (*ProxyResult, error) {
	err := fmt.Errorf("model %s does not support endpoint %s", strings.TrimSpace(model), strings.TrimSpace(endpoint))
	body, marshalErr := json.Marshal(map[string]any{"error": map[string]any{
		"message": err.Error(), "type": domains.ErrorModelNotSupported, "code": domains.ErrorModelNotSupported,
	}})
	if marshalErr != nil {
		return nil, marshalErr
	}
	return &ProxyResult{
		StatusCode: http.StatusBadRequest, Header: http.Header{"Content-Type": []string{"application/json"}},
		Body: body, ErrorType: domains.ErrorModelNotSupported, ErrorSummary: proxyErrorSummary(err),
	}, nil
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
	return &ProxyResult{
		StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: body,
		ErrorType: errorType, ErrorSummary: proxyErrorSummary(err), LatencyMs: latencyMs,
	}, nil
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
	if errors.Is(err, context.DeadlineExceeded) {
		return domains.ErrorUpstreamTimeout
	}
	if errors.Is(err, context.Canceled) {
		return domains.ErrorClientDisconnected
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		return domains.ErrorStreamIncomplete
	}
	var networkErr net.Error
	if errors.As(err, &networkErr) {
		if networkErr.Timeout() {
			return domains.ErrorUpstreamTimeout
		}
		return domains.ErrorNetwork
	}
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &syntaxErr) || errors.As(err, &typeErr) {
		return domains.ErrorProtocol
	}
	return classifyUnstructuredError(err.Error())
}

func classifyDownstreamWriteError(err error) string {
	if err == nil {
		return ""
	}
	return domains.ErrorClientDisconnected
}

// applyPostCompletionStreamError 将 response.completed 之后由下游取消造成的流结束
// 视为成功。此时完整响应和用量均已收到，取消只说明客户端没有继续等待连接收尾。
// 完成事件之前的取消以及超时、协议错误等上游异常仍按原错误返回。
func applyPostCompletionStreamError(result *ProxyResult, responseCompleted bool, err error) error {
	if err == nil || result == nil || !responseCompleted {
		return err
	}
	if classifyError(err) != domains.ErrorClientDisconnected {
		return err
	}
	result.ErrorType = ""
	result.ErrorSummary = ""
	result.DiagnosticType = domains.DiagnosticClientClosedAfterCompletion
	result.DiagnosticSummary = proxyErrorSummary(err)
	return nil
}

// applyStreamDoneWriteError 只豁免已收到 response.completed 后最终 [DONE] 标记的写入失败。
// 完成事件之前的任何下游写入失败仍然是 client_disconnected。
func applyStreamDoneWriteError(result *ProxyResult, responseCompleted bool, err error) error {
	if err == nil || result == nil {
		return err
	}
	if responseCompleted {
		result.DiagnosticType = domains.DiagnosticClientClosedAfterCompletion
		result.DiagnosticSummary = proxyErrorSummary(err)
		return nil
	}
	result.ErrorType = classifyDownstreamWriteError(err)
	result.ErrorSummary = proxyErrorSummary(err)
	return err
}

func streamEventErrorType(eventType string) string {
	switch eventType {
	case openai.EventResponseFailed, "error":
		return domains.ErrorUpstreamFailed
	case "response.incomplete":
		return domains.ErrorStreamIncomplete
	default:
		return ""
	}
}

func isRetryableInitialOverloadEvent(event openai.StreamEvent) bool {
	if streamEventErrorType(event.Type) == "" {
		return false
	}
	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
		Response struct {
			Error struct {
				Code    string `json:"code"`
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		} `json:"response"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return false
	}
	// Include the raw payload because response.failed may nest the error under
	// response.error instead of exposing it at the top level.
	text := strings.ToLower(strings.Join([]string{
		payload.Error.Code, payload.Error.Type, payload.Error.Message,
		payload.Response.Error.Code, payload.Response.Error.Type, payload.Response.Error.Message,
		payload.Code, payload.Message,
		string(event.Data),
	}, " "))
	return strings.Contains(text, "server_is_overloaded") ||
		strings.Contains(text, "service_unavailable_error") ||
		strings.Contains(text, "servers are currently overloaded")
}

func initialStreamErrorSummary(event openai.StreamEvent) string {
	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
		Response struct {
			Error struct {
				Code    string `json:"code"`
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		} `json:"response"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(event.Data, &payload)
	parts := []string{"event=" + strings.TrimSpace(event.Type), "openai"}
	if code := firstNonEmpty(payload.Error.Code, payload.Response.Error.Code, payload.Code); code != "" {
		parts = append(parts, "code="+code)
	}
	if errorType := firstNonEmpty(payload.Error.Type, payload.Response.Error.Type); errorType != "" {
		parts = append(parts, "type="+errorType)
	}
	if message := firstNonEmpty(payload.Error.Message, payload.Response.Error.Message, payload.Message); message != "" {
		parts = append(parts, "message="+message)
	}
	return normalizeErrorSummary(strings.Join(parts, " "))
}

// initialStreamErrorBody preserves the upstream error while another account
// is attempted. It is replayed only when routing exhausts all retry options.
func initialStreamErrorBody(req ProxyRequest, event openai.StreamEvent) []byte {
	if req.Endpoint == "/v1/chat/completions" {
		return []byte(fmt.Sprintf("data: %s\n\n", event.Data))
	}
	return []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", event.Type, event.Data))
}

func streamErrorSummary(eventType string, err error) string {
	summary := proxyErrorSummary(err)
	if strings.TrimSpace(eventType) == "" {
		return summary
	}
	return normalizeErrorSummary("event=" + strings.TrimSpace(eventType) + " " + summary)
}

func classifyUnstructuredError(message string) string {
	text := strings.ToLower(message)
	switch {
	case strings.Contains(text, "no_available_account"):
		return domains.ErrorNoAvailableAccount
	case strings.Contains(text, "model_not_supported") || strings.Contains(text, "record not found"):
		return domains.ErrorModelNotSupported
	case strings.Contains(text, "oauth") || strings.Contains(text, "refresh_token") || strings.Contains(text, "refresh token"):
		return domains.ErrorAuthFailed
	case strings.Contains(text, "quota") || strings.Contains(text, "insufficient"):
		return domains.ErrorQuotaExhausted
	case strings.Contains(text, "timeout") || strings.Contains(text, "deadline"):
		return domains.ErrorUpstreamTimeout
	case strings.Contains(text, "without response.completed") || strings.Contains(text, "stream ended") ||
		strings.Contains(text, "stream closed") || strings.Contains(text, "unexpected eof") ||
		strings.Contains(text, "response incomplete") || strings.Contains(text, "response.incomplete"):
		return domains.ErrorStreamIncomplete
	case strings.Contains(text, "response.failed") || strings.Contains(text, "stream event error"):
		return domains.ErrorUpstreamFailed
	case strings.Contains(text, "context canceled") || strings.Contains(text, "context cancelled") ||
		strings.Contains(text, "client disconnected"):
		return domains.ErrorClientDisconnected
	case strings.Contains(text, "invalid character") || strings.Contains(text, "cannot unmarshal") ||
		strings.Contains(text, "invalid json") || strings.Contains(text, "malformed") ||
		strings.Contains(text, "token too long"):
		return domains.ErrorProtocol
	case strings.Contains(text, "network") || strings.Contains(text, "connection") ||
		strings.Contains(text, "broken pipe") || strings.Contains(text, "dial tcp") ||
		strings.Contains(text, "no such host") || strings.Contains(text, "tls handshake"):
		return domains.ErrorNetwork
	case strings.Contains(text, "server_error") || strings.Contains(text, "internal server error") ||
		strings.Contains(text, "bad gateway") || strings.Contains(text, "service unavailable"):
		return domains.ErrorUpstreamHTTP5xx
	default:
		return domains.ErrorInternal
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
		return domains.ErrorUpstreamHTTP5xx
	case status >= 400:
		return domains.ErrorUpstreamHTTP4xx
	default:
		return classifyUnstructuredError(message)
	}
}

func proxyErrorSummary(err error) string {
	if err == nil {
		return ""
	}
	var openAIErr *openai.APIError
	if errors.As(err, &openAIErr) {
		return structuredErrorSummary("openai", openAIErr.StatusCode, openAIErr.Code, openAIErr.Type, openAIErr.Message, openAIErr.RequestID)
	}
	var chatGPTErr *chatgpt.APIError
	if errors.As(err, &chatGPTErr) {
		return structuredErrorSummary("chatgpt", chatGPTErr.StatusCode, chatGPTErr.Code, chatGPTErr.Type, chatGPTErr.Message, chatGPTErr.RequestID)
	}
	return normalizeErrorSummary(err.Error())
}

func structuredErrorSummary(source string, status int, code, errorType, message, requestID string) string {
	parts := []string{source}
	if status > 0 {
		parts = append(parts, fmt.Sprintf("status=%d", status))
	}
	if strings.TrimSpace(code) != "" {
		parts = append(parts, "code="+strings.TrimSpace(code))
	}
	if strings.TrimSpace(errorType) != "" {
		parts = append(parts, "type="+strings.TrimSpace(errorType))
	}
	if strings.TrimSpace(message) != "" {
		parts = append(parts, "message="+strings.TrimSpace(message))
	}
	if strings.TrimSpace(requestID) != "" {
		parts = append(parts, "upstream_request_id="+strings.TrimSpace(requestID))
	}
	return normalizeErrorSummary(strings.Join(parts, " "))
}

func normalizeErrorSummary(value string) string {
	const maxRunes = 512
	summary := strings.Join(strings.Fields(value), " ")
	runes := []rune(summary)
	if len(runes) <= maxRunes {
		return summary
	}
	return string(runes[:maxRunes-3]) + "..."
}
