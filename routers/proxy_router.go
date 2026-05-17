package routers

import "github.com/gin-gonic/gin"

type ProxyRouter struct{}

func (r ProxyRouter) InitProxyRouter(engine *gin.Engine) {
	v1 := engine.Group("/v1")
	{
		v1.GET("models", proxyApi.Models)
		v1.POST("chat/completions", proxyApi.ChatCompletions)
		v1.POST("responses", proxyApi.Responses)
		v1.POST("embeddings", proxyApi.Embeddings)
	}
}
