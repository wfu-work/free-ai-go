package main

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"freeai/inits"
)

//go:embed config.yaml
var defaultConfig []byte

func main() {
	if err := ensureDefaultConfig(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "prepare config failed: %v\n", err)
		os.Exit(1)
	}
	inits.Init()
}

func ensureDefaultConfig() error {
	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	configPath := filepath.Join(filepath.Dir(exePath), "config.yaml")
	if _, err = os.Stat(configPath); err == nil {
		setDefaultConfigEnv(configPath)
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err = os.WriteFile(configPath, defaultConfig, 0600); err != nil {
		return err
	}
	setDefaultConfigEnv(configPath)
	return nil
}

func setDefaultConfigEnv(configPath string) {
	if os.Getenv("NAV_CONFIG") != "" || hasConfigArg(os.Args[1:]) || localConfigExists() {
		return
	}
	_ = os.Setenv("NAV_CONFIG", configPath)
}

func localConfigExists() bool {
	for _, name := range []string{"config.debug.yaml", "config.release.yaml", "config.test.yaml", "config.yaml"} {
		if _, err := os.Stat(name); err == nil {
			return true
		}
	}
	return false
}

func hasConfigArg(args []string) bool {
	for i, arg := range args {
		if arg == "-c" || arg == "--c" {
			return i+1 < len(args)
		}
		if strings.HasPrefix(arg, "-c=") || strings.HasPrefix(arg, "--c=") {
			return true
		}
	}
	return false
}
