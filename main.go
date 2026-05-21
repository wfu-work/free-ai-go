package main

import (
	_ "embed"
	"fmt"
	"freeai/utils"
	"os"

	"freeai/inits"
)

//go:embed config.yaml
var defaultConfig []byte

func main() {
	if err := utils.NewDefaultConfigManager(defaultConfig).Ensure(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "prepare config failed: %v\n", err)
		os.Exit(1)
	}
	inits.Init()
}
