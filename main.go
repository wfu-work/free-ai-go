package main

import (
	"github.com/wfu-work/free-ai-go/inits"
)

// @title                       FREE AI Gateway
// @version                     v0.1.0
// @description                 AI API gateway backend based on nav-common-go-lib.
// @securityDefinitions.apikey  ApiKeyAuth
// @in                          header
// @name                        Authorization
// @BasePath                    /
func main() {
	inits.Init()
}
