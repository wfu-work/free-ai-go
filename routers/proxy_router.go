package routers

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/wfu-work/free-ai-go/services"
)

type ProxyRouter struct{}

func (r ProxyRouter) InitProxyRouter(engine *gin.Engine) {
	proxyPrefix := strings.TrimSpace(services.Config().ProxyPrefix)
	if proxyPrefix == "" {
		proxyPrefix = "/v1"
	}
	if !strings.HasPrefix(proxyPrefix, "/") {
		proxyPrefix = "/" + proxyPrefix
	}
	proxyPrefix = strings.TrimRight(proxyPrefix, "/")
	v1 := engine.Group(proxyPrefix)
	{
		v1.GET("models", proxyApi.Models)
		v1.POST("chat/completions", proxyApi.ChatCompletions)
		v1.POST("responses", proxyApi.Responses)
		v1.POST("embeddings", proxyApi.Embeddings)
	}
}
