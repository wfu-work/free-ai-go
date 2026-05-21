package utils

import (
	"os"
	"path/filepath"
	"strings"
)

const defaultConfigFileName = "config.yaml"

var localConfigFileNames = []string{
	"config.debug.yaml",
	"config.release.yaml",
	"config.test.yaml",
	defaultConfigFileName,
}

type DefaultConfigManager struct {
	defaultConfig []byte
}

func NewDefaultConfigManager(defaultConfig []byte) DefaultConfigManager {
	return DefaultConfigManager{defaultConfig: defaultConfig}
}

func (m DefaultConfigManager) Ensure() error {
	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	configPath := filepath.Join(filepath.Dir(exePath), defaultConfigFileName)
	if _, err = os.Stat(configPath); err == nil {
		m.SetDefaultConfigEnv(configPath)
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err = os.WriteFile(configPath, m.defaultConfig, 0600); err != nil {
		return err
	}
	m.SetDefaultConfigEnv(configPath)
	return nil
}

func (m DefaultConfigManager) SetDefaultConfigEnv(configPath string) {
	if os.Getenv("NAV_CONFIG") != "" || HasConfigArg(os.Args[1:]) || LocalConfigExists() {
		return
	}
	_ = os.Setenv("NAV_CONFIG", configPath)
}

func LocalConfigExists() bool {
	for _, name := range localConfigFileNames {
		if _, err := os.Stat(name); err == nil {
			return true
		}
	}
	return false
}

func HasConfigArg(args []string) bool {
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
