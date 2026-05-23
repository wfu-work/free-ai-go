package utils

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"go.yaml.in/yaml/v3"
)

const defaultConfigFileName = "config.yaml"
const defaultAppDataDirName = "FreeAi"

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
	if err := normalizeExplicitConfigEnv(); err != nil {
		return err
	}
	if normalizeConfigArgs(os.Args[1:]) {
		return nil
	}
	if os.Getenv("NAV_CONFIG") != "" || LocalConfigExists() {
		return nil
	}

	configPath, err := DefaultConfigPath()
	if err != nil {
		return err
	}
	if _, err = os.Stat(configPath); err == nil {
		if err = m.EnsurePortableConfig(configPath); err != nil {
			return err
		}
		m.SetDefaultConfigEnv(configPath)
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
		return err
	}
	configBytes, err := m.MaterializeDefaultConfig(filepath.Dir(configPath))
	if err != nil {
		return err
	}
	if err = os.WriteFile(configPath, configBytes, 0600); err != nil {
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

func DefaultConfigPath() (string, error) {
	if !productionBuild {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		return filepath.Join(wd, defaultConfigFileName), nil
	}
	dir, err := AppDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, defaultConfigFileName), nil
}

func AppDataDir() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("FREEAI_DATA_DIR")); dir != "" {
		return filepath.Abs(filepath.FromSlash(dir))
	}
	if dir := strings.TrimSpace(os.Getenv("FREEAI_HOME")); dir != "" {
		return filepath.Abs(filepath.FromSlash(dir))
	}

	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", defaultAppDataDirName), nil
	case "windows":
		if dir := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); dir != "" {
			return filepath.Join(dir, defaultAppDataDirName), nil
		}
		if dir := strings.TrimSpace(os.Getenv("APPDATA")); dir != "" {
			return filepath.Join(dir, defaultAppDataDirName), nil
		}
	}

	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, defaultAppDataDirName), nil
}

func (m DefaultConfigManager) MaterializeDefaultConfig(baseDir string) ([]byte, error) {
	var cfg map[string]any
	decoder := yaml.NewDecoder(bytes.NewReader(m.defaultConfig))
	if err := decoder.Decode(&cfg); err != nil {
		return nil, err
	}

	dataDir := filepath.Join(baseDir, "data")
	setConfigPath(cfg, []string{"sqlite", "path"}, dataDir)
	setConfigPath(cfg, []string{"local", "oss-path"}, filepath.Join(dataDir, "oss"))
	setConfigPath(cfg, []string{"local", "cache-path"}, filepath.Join(dataDir, "cache.json"))
	setConfigPath(cfg, []string{"local", "ip2geo-path"}, filepath.Join(dataDir, "ip2geo"))
	setConfigPath(cfg, []string{"zap", "director"}, filepath.Join(baseDir, "logback"))
	setConfigPath(cfg, []string{"freeai", "secret-key-file"}, filepath.Join(dataDir, "master.key"))

	out, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (m DefaultConfigManager) EnsurePortableConfig(configPath string) error {
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	var cfg map[string]any
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&cfg); err != nil {
		return err
	}

	baseDir := filepath.Dir(configPath)
	dataDir := filepath.Join(baseDir, "data")
	changed := false
	changed = upgradeLegacyConfigPath(cfg, []string{"sqlite", "path"}, dataDir) || changed
	changed = upgradeLegacyConfigPath(cfg, []string{"local", "oss-path"}, filepath.Join(dataDir, "oss")) || changed
	changed = upgradeLegacyConfigPath(cfg, []string{"local", "cache-path"}, filepath.Join(dataDir, "cache.json")) || changed
	changed = upgradeLegacyConfigPath(cfg, []string{"local", "ip2geo-path"}, filepath.Join(dataDir, "ip2geo")) || changed
	changed = upgradeLegacyConfigPath(cfg, []string{"zap", "director"}, filepath.Join(baseDir, "logback")) || changed
	changed = upgradeLegacyConfigPath(cfg, []string{"freeai", "secret-key-file"}, filepath.Join(dataDir, "master.key")) || changed
	if !changed {
		return nil
	}

	out, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, out, 0600)
}

func setConfigPath(cfg map[string]any, keys []string, value string) {
	if len(keys) == 0 {
		return
	}
	current := cfg
	for _, key := range keys[:len(keys)-1] {
		next, ok := current[key].(map[string]any)
		if !ok {
			next = map[string]any{}
			current[key] = next
		}
		current = next
	}
	current[keys[len(keys)-1]] = filepath.Clean(value)
}

func upgradeLegacyConfigPath(cfg map[string]any, keys []string, value string) bool {
	current := cfg
	for _, key := range keys[:len(keys)-1] {
		next, ok := current[key].(map[string]any)
		if !ok {
			return false
		}
		current = next
	}

	key := keys[len(keys)-1]
	currentValue, ok := current[key].(string)
	if !ok || !isLegacyRelativePath(currentValue) {
		return false
	}
	current[key] = filepath.Clean(value)
	return true
}

func isLegacyRelativePath(path string) bool {
	path = strings.TrimSpace(filepath.ToSlash(path))
	path = strings.TrimSuffix(path, "/")
	switch path {
	case "./data", "data", "./data/oss", "data/oss", "./data/cache.json", "data/cache.json", "./data/ip2geo", "data/ip2geo", "./data/master.key", "data/master.key", "logback", "./logback":
		return true
	default:
		return false
	}
}

func normalizeExplicitConfigEnv() error {
	configPath := strings.TrimSpace(os.Getenv("NAV_CONFIG"))
	if configPath == "" || filepath.IsAbs(configPath) {
		return nil
	}
	abs, err := filepath.Abs(configPath)
	if err != nil {
		return err
	}
	return os.Setenv("NAV_CONFIG", abs)
}

func normalizeConfigArgs(args []string) bool {
	for i, arg := range args {
		if (arg == "-c" || arg == "--c") && i+1 < len(args) {
			os.Args[i+2] = absPathOrOriginal(args[i+1])
			return true
		}
		if strings.HasPrefix(arg, "-c=") {
			os.Args[i+1] = "-c=" + absPathOrOriginal(strings.TrimPrefix(arg, "-c="))
			return true
		}
		if strings.HasPrefix(arg, "--c=") {
			os.Args[i+1] = "--c=" + absPathOrOriginal(strings.TrimPrefix(arg, "--c="))
			return true
		}
	}
	return false
}

func absPathOrOriginal(path string) string {
	if strings.TrimSpace(path) == "" || filepath.IsAbs(path) {
		return path
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}
