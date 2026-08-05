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

	"github.com/wfu-work/free-ai-go/domains"
	"github.com/wfu-work/proxy-api-lib/catalog"
	"github.com/wfu-work/proxy-api-lib/openai"
)

const (
	openAIAPIBaseURL        = "https://api.openai.com/v1"
	maxOpenAIModelBodyBytes = 8 << 20
	maxImageResponseBytes   = 64 << 20
)

// OpenAIImageService 使用独立 OpenAI Platform API Key 访问官方图片模型。
// ChatGPT/Codex OAuth 凭据不会进入这条链路。
type OpenAIImageService struct{}

var OpenAIImageServiceApp = OpenAIImageService{}

type openAIModelList struct {
	Data []json.RawMessage `json:"data"`
}

type openAIModelMetadata struct {
	ID      string `json:"id"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// ListModels 返回 API Key 实际可见的图片生成模型。
func (OpenAIImageService) ListModels(ctx context.Context, apiKey string) ([]catalog.RemoteModel, error) {
	client, err := UpstreamHTTPClient()
	if err != nil {
		return nil, err
	}
	return listOpenAIImageModels(ctx, client, openAIAPIBaseURL, apiKey)
}

func listOpenAIImageModels(ctx context.Context, client *http.Client, baseURL, apiKey string) ([]catalog.RemoteModel, error) {
	req, err := newOpenAIAPIRequest(ctx, http.MethodGet, baseURL, "/models", apiKey, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := readLimitedBody(resp.Body, maxOpenAIModelBodyBytes)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseOpenAIAPIError(resp.StatusCode, resp.Header.Get("x-request-id"), body)
	}
	var envelope openAIModelList
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode OpenAI model list: %w", err)
	}
	models := make([]catalog.RemoteModel, 0)
	capabilities := json.RawMessage(`{"input_modalities":["text","image"],"output_modalities":["image"],"supported_endpoints":["/v1/images/generations"]}`)
	for _, raw := range envelope.Data {
		var model openAIModelMetadata
		if err := json.Unmarshal(raw, &model); err != nil {
			continue
		}
		model.ID = strings.TrimSpace(model.ID)
		if !isOpenAIImageModel(model.ID) {
			continue
		}
		models = append(models, catalog.RemoteModel{
			ID: model.ID, DisplayName: model.ID, OwnedBy: model.OwnedBy, Created: model.Created,
			Capabilities: append(json.RawMessage(nil), capabilities...), Raw: append(json.RawMessage(nil), raw...),
		})
	}
	if len(models) == 0 {
		return nil, errors.New("OpenAI API key has no visible image generation models")
	}
	return models, nil
}

func isOpenAIImageModel(model string) bool {
	value := strings.ToLower(strings.TrimSpace(model))
	return strings.Contains(value, "image") || strings.HasPrefix(value, "dall-e")
}

// Generate 原样转发 Images API JSON，请求参数和响应字段保持官方兼容。
func (OpenAIImageService) Generate(ctx context.Context, account domains.Account, body []byte) (*ProxyResult, error) {
	apiKey, err := AccountServiceApp.LoadAPIKey(account)
	if err != nil {
		return apiErrorResult(err, 0)
	}
	client, err := UpstreamHTTPClient()
	if err != nil {
		return apiErrorResult(err, 0)
	}
	return generateOpenAIImage(ctx, client, openAIAPIBaseURL, apiKey, body)
}

func generateOpenAIImage(ctx context.Context, client *http.Client, baseURL, apiKey string, body []byte) (*ProxyResult, error) {
	started := time.Now()
	request, err := newOpenAIAPIRequest(ctx, http.MethodPost, baseURL, "/images/generations", apiKey, body)
	if err != nil {
		return apiErrorResult(err, time.Since(started).Milliseconds())
	}
	preparationMs := time.Since(started).Milliseconds()
	trace := newUpstreamRequestTrace()
	request = request.WithContext(trace.context(ctx))
	resp, err := client.Do(request)
	if err != nil {
		result, proxyErr := apiErrorResult(err, time.Since(started).Milliseconds())
		applyUpstreamTiming(result, preparationMs, trace)
		return result, proxyErr
	}
	defer resp.Body.Close()
	responseBody, readErr := readLimitedBody(resp.Body, maxImageResponseBytes)
	latencyMs := time.Since(started).Milliseconds()
	if readErr != nil {
		result, proxyErr := apiErrorResult(readErr, latencyMs)
		applyUpstreamTiming(result, preparationMs, trace)
		return result, proxyErr
	}
	result := &ProxyResult{
		StatusCode: resp.StatusCode,
		Header:     resp.Header.Clone(),
		Body:       responseBody,
		LatencyMs:  latencyMs,
		Usage:      imageUsage(responseBody),
	}
	applyUpstreamTiming(result, preparationMs, trace)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		upstreamErr := parseOpenAIAPIError(resp.StatusCode, resp.Header.Get("x-request-id"), responseBody)
		result.ErrorType = classifyError(upstreamErr)
		result.ErrorSummary = proxyErrorSummary(upstreamErr)
	}
	return result, nil
}

func newOpenAIAPIRequest(ctx context.Context, method, baseURL, path, apiKey string, body []byte) (*http.Request, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, errors.New("OpenAI API key is empty")
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = openAIAPIBaseURL
	}
	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func readLimitedBody(reader io.Reader, limit int64) ([]byte, error) {
	if limit <= 0 {
		return io.ReadAll(reader)
	}
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("upstream response exceeds %d bytes", limit)
	}
	return body, nil
}

func parseOpenAIAPIError(status int, requestID string, body []byte) error {
	var envelope struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &envelope)
	message := strings.TrimSpace(envelope.Error.Message)
	if message == "" {
		message = strings.TrimSpace(string(body))
	}
	if message == "" {
		message = http.StatusText(status)
	}
	return &openai.APIError{
		StatusCode: status, Code: strings.TrimSpace(envelope.Error.Code),
		Type: strings.TrimSpace(envelope.Error.Type), Message: message, RequestID: strings.TrimSpace(requestID),
	}
}

func imageUsage(body []byte) ProxyUsage {
	var envelope struct {
		Usage struct {
			InputTokens  int64 `json:"input_tokens"`
			OutputTokens int64 `json:"output_tokens"`
			TotalTokens  int64 `json:"total_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		return ProxyUsage{}
	}
	usage := ProxyUsage{InputTokens: envelope.Usage.InputTokens, OutputTokens: envelope.Usage.OutputTokens}
	if usage.InputTokens == 0 && usage.OutputTokens == 0 && envelope.Usage.TotalTokens > 0 {
		usage.OutputTokens = envelope.Usage.TotalTokens
	}
	return usage
}
