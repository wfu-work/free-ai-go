package apis

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"freeai/services"

	"github.com/gin-gonic/gin"
)

type ProxyApi struct{}

func (a ProxyApi) Models(c *gin.Context) {
	key, err := services.PlatformKeyServiceApp.Verify(c.GetHeader("Authorization"))
	if err != nil {
		c.JSON(http.StatusUnauthorized, openAIError("platform_key_invalid", err.Error()))
		return
	}
	models, err := services.ModelServiceApp.ListEnabled()
	if err != nil {
		c.JSON(http.StatusInternalServerError, openAIError("server_error", err.Error()))
		return
	}
	data := make([]gin.H, 0, len(models))
	for _, model := range models {
		if !services.PlatformKeyServiceApp.ModelAllowed(key, model.PublicModel) {
			continue
		}
		data = append(data, gin.H{
			"id":       model.PublicModel,
			"object":   "model",
			"owned_by": model.Provider,
		})
	}
	c.JSON(http.StatusOK, gin.H{"object": "list", "data": data})
}

func (a ProxyApi) ChatCompletions(c *gin.Context) {
	a.forward(c, "/v1/chat/completions")
}

func (a ProxyApi) Responses(c *gin.Context) {
	a.forward(c, "/v1/responses")
}

func (a ProxyApi) Embeddings(c *gin.Context) {
	a.forward(c, "/v1/embeddings")
}

func (a ProxyApi) forward(c *gin.Context, endpoint string) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, openAIError("invalid_request_error", err.Error()))
		return
	}
	if model := readModel(body); model != "" {
		c.Request.Header.Set("X-FreeModel-Model", model)
	}
	stream := strings.Contains(string(body), `"stream":true`)
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

func openAIError(code, message string) gin.H {
	return gin.H{"error": gin.H{
		"message": message,
		"type":    code,
		"code":    code,
	}}
}
