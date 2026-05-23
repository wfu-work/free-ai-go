package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestHasConfigArg(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{name: "short separated", args: []string{"-c", "custom.yaml"}, want: true},
		{name: "short equals", args: []string{"-c=custom.yaml"}, want: true},
		{name: "long separated", args: []string{"--c", "custom.yaml"}, want: true},
		{name: "long equals", args: []string{"--c=custom.yaml"}, want: true},
		{name: "missing value", args: []string{"-c"}, want: false},
		{name: "none", args: []string{"serve"}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := HasConfigArg(tc.args); got != tc.want {
				t.Fatalf("HasConfigArg(%v) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

func TestSetDefaultConfigEnvRespectsExplicitEnv(t *testing.T) {
	t.Setenv("NAV_CONFIG", "custom.yaml")
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"freeai"}

	NewDefaultConfigManager([]byte("system:\n  app-name: test\n")).SetDefaultConfigEnv("embedded.yaml")

	if got := os.Getenv("NAV_CONFIG"); got != "custom.yaml" {
		t.Fatalf("NAV_CONFIG = %q, want explicit env", got)
	}
}

func TestSetDefaultConfigEnvRespectsConfigArg(t *testing.T) {
	t.Setenv("NAV_CONFIG", "")
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"freeai", "-c", "custom.yaml"}

	NewDefaultConfigManager([]byte("system:\n  app-name: test\n")).SetDefaultConfigEnv("embedded.yaml")

	if got := os.Getenv("NAV_CONFIG"); got != "" {
		t.Fatalf("NAV_CONFIG = %q, want empty because -c is explicit", got)
	}
}

func TestSetDefaultConfigEnvRespectsLocalConfig(t *testing.T) {
	t.Setenv("NAV_CONFIG", "")
	oldArgs := os.Args
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		os.Args = oldArgs
		_ = os.Chdir(oldWd)
	})
	os.Args = []string{"freeai"}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	if err := os.WriteFile("config.yaml", []byte("system:\n  app-name: test\n"), 0600); err != nil {
		t.Fatalf("write local config: %v", err)
	}

	NewDefaultConfigManager([]byte("system:\n  app-name: test\n")).SetDefaultConfigEnv("embedded.yaml")

	if got := os.Getenv("NAV_CONFIG"); got != "" {
		t.Fatalf("NAV_CONFIG = %q, want empty because local config exists", got)
	}
}

func TestMaterializeDefaultConfigUsesPortablePaths(t *testing.T) {
	baseDir := t.TempDir()
	defaultConfig := []byte(`
system:
  app-name: test
local:
  oss-path: ./data/oss
sqlite:
  path: ./data/
zap:
  director: logback
freeai:
  secret-key-file: ./data/master.key
`)

	raw, err := NewDefaultConfigManager(defaultConfig).MaterializeDefaultConfig(baseDir)
	if err != nil {
		t.Fatalf("materialize config: %v", err)
	}

	cfg := readConfigMap(t, raw)
	assertConfigPath(t, cfg, []string{"sqlite", "path"}, filepath.Join(baseDir, "data"))
	assertConfigPath(t, cfg, []string{"local", "oss-path"}, filepath.Join(baseDir, "data", "oss"))
	assertConfigPath(t, cfg, []string{"local", "cache-path"}, filepath.Join(baseDir, "data", "cache.json"))
	assertConfigPath(t, cfg, []string{"local", "ip2geo-path"}, filepath.Join(baseDir, "data", "ip2geo"))
	assertConfigPath(t, cfg, []string{"zap", "director"}, filepath.Join(baseDir, "logback"))
	assertConfigPath(t, cfg, []string{"freeai", "secret-key-file"}, filepath.Join(baseDir, "data", "master.key"))
}

func TestEnsurePortableConfigUpgradesLegacyRelativePaths(t *testing.T) {
	baseDir := t.TempDir()
	configPath := filepath.Join(baseDir, "config.yaml")
	raw := []byte(`
system:
  app-name: test
local:
  oss-path: ./data/oss
  cache-path: ./data/cache.json
sqlite:
  path: ./data/
zap:
  director: logback
freeai:
  secret-key-file: ./data/master.key
`)
	if err := os.WriteFile(configPath, raw, 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if err := NewDefaultConfigManager(nil).EnsurePortableConfig(configPath); err != nil {
		t.Fatalf("ensure portable config: %v", err)
	}

	updated, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	cfg := readConfigMap(t, updated)
	assertConfigPath(t, cfg, []string{"sqlite", "path"}, filepath.Join(baseDir, "data"))
	assertConfigPath(t, cfg, []string{"local", "oss-path"}, filepath.Join(baseDir, "data", "oss"))
	assertConfigPath(t, cfg, []string{"local", "cache-path"}, filepath.Join(baseDir, "data", "cache.json"))
	assertConfigPath(t, cfg, []string{"zap", "director"}, filepath.Join(baseDir, "logback"))
	assertConfigPath(t, cfg, []string{"freeai", "secret-key-file"}, filepath.Join(baseDir, "data", "master.key"))
}

func TestEnsurePortableConfigKeepsCustomRelativePaths(t *testing.T) {
	baseDir := t.TempDir()
	configPath := filepath.Join(baseDir, "config.yaml")
	raw := []byte(`
sqlite:
  path: ./custom-db
freeai:
  secret-key-file: ./secrets/master.key
`)
	if err := os.WriteFile(configPath, raw, 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if err := NewDefaultConfigManager(nil).EnsurePortableConfig(configPath); err != nil {
		t.Fatalf("ensure portable config: %v", err)
	}

	updated, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(updated), "./custom-db") {
		t.Fatalf("custom sqlite path was unexpectedly changed:\n%s", string(updated))
	}
	if !strings.Contains(string(updated), "./secrets/master.key") {
		t.Fatalf("custom secret path was unexpectedly changed:\n%s", string(updated))
	}
}

func readConfigMap(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var cfg map[string]any
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	return cfg
}

func assertConfigPath(t *testing.T, cfg map[string]any, keys []string, want string) {
	t.Helper()
	current := cfg
	for _, key := range keys[:len(keys)-1] {
		next, ok := current[key].(map[string]any)
		if !ok {
			t.Fatalf("%s is missing or invalid", strings.Join(keys, "."))
		}
		current = next
	}
	got, _ := current[keys[len(keys)-1]].(string)
	if got != filepath.Clean(want) {
		t.Fatalf("%s = %q, want %q", strings.Join(keys, "."), got, filepath.Clean(want))
	}
}
