package apis

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"freeai/services"

	"github.com/gin-gonic/gin"
)

type ProxyApi struct{}

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
	path := c.Request.URL.Path
	key, err := services.PlatformKeyServiceApp.Verify(c.GetHeader("Authorization"))
	if err != nil {
		services.RequestLogServiceApp.Record(services.RequestLogInput{
			Method:     c.Request.Method,
			Path:       path,
			KeyPrefix:  services.PlatformKeyPrefixFromHeader(c.GetHeader("Authorization")),
			StatusCode: http.StatusUnauthorized,
			ErrorType:  "platform_key_invalid",
			LatencyMs:  time.Since(start).Milliseconds(),
		})
		c.JSON(http.StatusUnauthorized, openAIError("platform_key_invalid", err.Error()))
		return
	}
	models, err := services.ModelServiceApp.ListEnabled()
	if err != nil {
		services.RequestLogServiceApp.Record(services.RequestLogInput{
			Method:        c.Request.Method,
			Path:          path,
			PlatformKeyID: key.Guid,
			PlatformKey:   key.Name,
			KeyPrefix:     key.KeyPrefix,
			StatusCode:    http.StatusInternalServerError,
			ErrorType:     "server_error",
			LatencyMs:     time.Since(start).Milliseconds(),
		})
		c.JSON(http.StatusInternalServerError, openAIError("server_error", err.Error()))
		return
	}
	data := make([]gin.H, 0, len(models))
	for _, model := range models {
		if !services.PlatformKeyServiceApp.ModelMappingAllowed(key, model) {
			continue
		}
		for _, name := range services.ModelServiceApp.PublicNames(model) {
			data = append(data, gin.H{
				"id":       name,
				"object":   "model",
				"owned_by": model.Provider,
			})
		}
	}
	services.RequestLogServiceApp.Record(services.RequestLogInput{
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

// Embeddings OpenAI Embeddings代理
// @Summary OpenAI Embeddings代理
// @Description OpenAI兼容Embeddings代理入口
// @Tags 代理模块
// @Accept json
// @Produce json
// @Router /v1/embeddings [post]
func (a ProxyApi) Embeddings(c *gin.Context) {
	forwardProxy(c, "/v1/embeddings")
}

func forwardProxy(c *gin.Context, endpoint string) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, openAIError("invalid_request_error", err.Error()))
		return
	}
	if model := readModel(body); model != "" {
		c.Request.Header.Set("X-FreeAi-Model", model)
	}
	stream := readStream(body)
	if stream {
		c.Writer.Header().Set("Content-Type", "text/event-stream")
		c.Writer.Header().Set("Cache-Control", "no-cache")
		c.Writer.Header().Set("Connection", "keep-alive")
	}
	out, err := proxyService.Handle(c.Request, c.Writer, endpoint, body, stream)
	if err != nil {
		if stream && out.StatusCode >= 200 && out.StatusCode < 300 {
			return
		}
		status := out.StatusCode
		if status == 0 {
			status = http.StatusBadGateway
		}
		c.JSON(status, openAIError("proxy_error", err.Error()))
		return
	}
	if out.Header != nil {
		for k, values := range out.Header {
			for _, value := range values {
				c.Writer.Header().Add(k, value)
			}
		}
	}
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

func readModel(body []byte) string {
	var payload struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal(body, &payload)
	return payload.Model
}

func readStream(body []byte) bool {
	var payload struct {
		Stream bool `json:"stream"`
	}
	_ = json.Unmarshal(body, &payload)
	return payload.Stream
}

func openAIError(code, message string) gin.H {
	return gin.H{"error": gin.H{
		"message": message,
		"type":    code,
		"code":    code,
	}}
}
