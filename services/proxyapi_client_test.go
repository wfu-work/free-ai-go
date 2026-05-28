package services

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	proxydomains "github.com/wfu-work/proxy-api-lib/domains"
)

func TestChatCompletionResponseConvertsResponsesText(t *testing.T) {
	body, err := json.Marshal(chatCompletionResponse("gpt-test", &proxydomains.Response{
		ID:    "resp_123",
		Model: "gpt-test",
		Output: []proxydomains.ResponseItem{
			{
				Type: "message",
				Content: []proxydomains.ResponseContent{
					{Type: "output_text", Text: "hello"},
				},
			},
		},
		Usage: &proxydomains.Usage{InputTokens: 2, OutputTokens: 3, TotalTokens: 5},
	}))
	if err != nil {
		t.Fatalf("marshal chat response: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal chat response: %v", err)
	}
	if payload["object"] != "chat.completion" {
		t.Fatalf("object = %v", payload["object"])
	}
	choices := payload["choices"].([]any)
	message := choices[0].(map[string]any)["message"].(map[string]any)
	if message["content"] != "hello" {
		t.Fatalf("content = %v", message["content"])
	}
}

func TestWriteChatCompletionChunk(t *testing.T) {
	var buf bytes.Buffer
	err := writeChatCompletionChunk(&buf, "gpt-test", map[string]any{"content": "hi"}, "")
	if err != nil {
		t.Fatalf("writeChatCompletionChunk: %v", err)
	}
	out := buf.String()
	if !strings.HasPrefix(out, "data: ") {
		t.Fatalf("chunk prefix = %q", out)
	}
	if !strings.Contains(out, `"content":"hi"`) {
		t.Fatalf("chunk = %q", out)
	}
}

func TestClassifyHTTPStatusPrefersQuotaExhaustedOverRateLimit(t *testing.T) {
	got := classifyHTTPStatus(http.StatusTooManyRequests, []byte(`{"error":"insufficient quota"}`))
	if got != "quota_exhausted" {
		t.Fatalf("classifyHTTPStatus = %q", got)
	}
}

func TestClassifyAPIErrorPrefersQuotaExhaustedOverRateLimit(t *testing.T) {
	got := classifyAPIError(&proxydomains.APIError{
		StatusCode: http.StatusTooManyRequests,
		Message:    "quota is insufficient",
	})
	if got != "quota_exhausted" {
		t.Fatalf("classifyAPIError = %q", got)
	}
}

func TestLoginCallbackAccessTokenPrefersAPIKeyAccessToken(t *testing.T) {
	got := loginCallbackAccessToken(`{"api_key_access_token":"api-key-token","access_token":"oauth-token"}`)
	if got != "api-key-token" {
		t.Fatalf("loginCallbackAccessToken = %q", got)
	}
}

func TestLoginCallbackAccessTokenFallsBackToOAuthAccessToken(t *testing.T) {
	got := loginCallbackAccessToken(`{"access_token":"oauth-token"}`)
	if got != "oauth-token" {
		t.Fatalf("loginCallbackAccessToken = %q", got)
	}
}
