package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"freeai/domains"

	"github.com/pelletier/go-toml/v2"
)

type CodexConfigService struct{}

var CodexConfigServiceApp = CodexConfigService{}

type CodexConfigInput struct {
	PlatformKeyGuid string `json:"platformKeyGuid"`
	APIBaseURL      string `json:"apiBaseUrl"`
	Model           string `json:"model"`
	ProviderName    string `json:"providerName"`
	ReasoningEffort string `json:"reasoningEffort"`
	WriteGlobal     bool   `json:"writeGlobal"`
}

type CodexConfigOutput struct {
	AuthPath        string `json:"authPath"`
	ConfigPath      string `json:"configPath"`
	AuthJSON        string `json:"authJson"`
	ConfigTOML      string `json:"configToml"`
	Model           string `json:"model"`
	ProviderName    string `json:"providerName"`
	APIBaseURL      string `json:"apiBaseUrl"`
	PlatformKey     string `json:"platformKey"`
	PlatformKeyID   string `json:"platformKeyId"`
	PlatformKeyName string `json:"platformKeyName"`
	AppliedAt       int64  `json:"appliedAt,omitempty"`
}

func (s CodexConfigService) Preview(input CodexConfigInput) (CodexConfigOutput, error) {
	key, err := s.validatePlatformKey(input.PlatformKeyGuid)
	if err != nil {
		return CodexConfigOutput{}, err
	}
	paths, err := codexConfigPaths()
	if err != nil {
		return CodexConfigOutput{}, err
	}
	authJSON, err := buildCodexAuthPreview(key.Key)
	if err != nil {
		return CodexConfigOutput{}, err
	}
	model := normalizeCodexModel(input.Model, key)
	providerName := normalizeCodexProviderName(input.ProviderName)
	apiBaseURL := normalizeCodexAPIBaseURL(input.APIBaseURL)
	rawConfig, _ := os.ReadFile(paths.configPath)
	configTOML, err := buildCodexConfig(string(rawConfig), providerName, apiBaseURL, model, input.ReasoningEffort, input.WriteGlobal)
	if err != nil {
		return CodexConfigOutput{}, err
	}
	return CodexConfigOutput{
		AuthPath:        paths.authPath,
		ConfigPath:      paths.configPath,
		AuthJSON:        authJSON,
		ConfigTOML:      configTOML,
		Model:           model,
		ProviderName:    providerName,
		APIBaseURL:      apiBaseURL,
		PlatformKey:     key.Key,
		PlatformKeyID:   key.Guid,
		PlatformKeyName: key.Name,
	}, nil
}

func (s CodexConfigService) Apply(input CodexConfigInput) (CodexConfigOutput, error) {
	preview, err := s.Preview(input)
	if err != nil {
		return CodexConfigOutput{}, err
	}
	if err := os.MkdirAll(filepath.Dir(preview.AuthPath), 0700); err != nil {
		return CodexConfigOutput{}, err
	}
	if err := mergeCodexAuth(preview.AuthPath, preview.PlatformKey); err != nil {
		return CodexConfigOutput{}, err
	}
	if err := os.WriteFile(preview.ConfigPath, []byte(preview.ConfigTOML), 0600); err != nil {
		return CodexConfigOutput{}, err
	}
	preview.AppliedAt = time.Now().UnixMilli()
	return preview, nil
}

func (s CodexConfigService) validatePlatformKey(guid string) (domains.PlatformKey, error) {
	guid = strings.TrimSpace(guid)
	if guid == "" {
		return domains.PlatformKey{}, errors.New("platformKeyGuid is required")
	}
	key, err := PlatformKeyServiceApp.GetByGuid(guid)
	if err != nil {
		return domains.PlatformKey{}, err
	}
	if !key.Enabled {
		return domains.PlatformKey{}, errors.New("platform key is disabled")
	}
	if normalizeProtocolType(key.ProtocolType) != "openai_compatible" {
		return domains.PlatformKey{}, errors.New("only OpenAI Compat platform keys can be written to Codex")
	}
	if strings.TrimSpace(key.Key) == "" {
		return domains.PlatformKey{}, errors.New("platform key secret is empty")
	}
	return key, nil
}

type codexPaths struct {
	authPath   string
	configPath string
}

func codexConfigPaths() (codexPaths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return codexPaths{}, err
	}
	base := filepath.Join(home, ".codex")
	return codexPaths{
		authPath:   filepath.Join(base, "auth.json"),
		configPath: filepath.Join(base, "config.toml"),
	}, nil
}

func buildCodexAuthPreview(apiKey string) (string, error) {
	payload := map[string]any{
		"OPENAI_API_KEY": strings.TrimSpace(apiKey),
		"last_refresh":   time.Now().UTC().Format(time.RFC3339Nano),
	}
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", err
	}
	return string(body) + "\n", nil
}

func mergeCodexAuth(path, apiKey string) error {
	payload := map[string]any{}
	if body, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(body))) > 0 {
		_ = json.Unmarshal(body, &payload)
	}
	payload["OPENAI_API_KEY"] = strings.TrimSpace(apiKey)
	payload["last_refresh"] = time.Now().UTC().Format(time.RFC3339Nano)
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(body, '\n'), 0600)
}

func buildCodexConfig(raw, providerName, apiBaseURL, model, reasoningEffort string, writeGlobal bool) (string, error) {
	next := strings.TrimSpace(raw)
	if next == "" {
		next = "[model_providers]\n"
	}
	if writeGlobal {
		next = upsertTopLevelTomlValue(next, "model_provider", providerName)
		next = upsertTopLevelTomlValue(next, "model", model)
		next = upsertTopLevelTomlValue(next, "disable_response_storage", true)
		if strings.TrimSpace(reasoningEffort) != "" {
			next = upsertTopLevelTomlValue(next, "model_reasoning_effort", strings.TrimSpace(reasoningEffort))
		}
	}
	next = upsertTomlTableValues(next, "model_providers."+providerName, map[string]any{
		"name":                 providerName,
		"wire_api":             "responses",
		"requires_openai_auth": true,
		"base_url":             apiBaseURL,
	})
	var decoded map[string]any
	if err := toml.Unmarshal([]byte(next), &decoded); err != nil {
		return "", fmt.Errorf("generated codex config is invalid: %w", err)
	}
	if !strings.HasSuffix(next, "\n") {
		next += "\n"
	}
	return next, nil
}

func normalizeCodexProviderName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "custom"
	}
	value = strings.ToLower(value)
	replacer := strings.NewReplacer(" ", "-", "_", "-", ".", "-", "/", "-")
	return strings.Trim(replacer.Replace(value), "-")
}

func normalizeCodexAPIBaseURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "http://localhost:8787/v1"
	}
	value = strings.TrimRight(value, "/")
	if !strings.HasSuffix(value, "/v1") {
		value += "/v1"
	}
	return value
}

func normalizeCodexModel(value string, key domains.PlatformKey) string {
	value = strings.TrimSpace(value)
	if value != "" {
		return value
	}
	if strings.TrimSpace(key.BoundModel) != "" {
		return strings.TrimSpace(key.BoundModel)
	}
	for _, model := range parseAllowedModels(key.AllowedModels) {
		if model != "" && model != "*" {
			return model
		}
	}
	return "gpt-5.5"
}

func parseAllowedModels(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var models []string
	if err := json.Unmarshal([]byte(raw), &models); err == nil {
		return models
	}
	for _, item := range strings.Split(raw, ",") {
		if model := strings.TrimSpace(item); model != "" {
			models = append(models, model)
		}
	}
	return models
}

func upsertTopLevelTomlValue(raw, key string, value any) string {
	lines := strings.Split(raw, "\n")
	insertAt := len(lines)
	replaced := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			insertAt = i
			break
		}
		if strings.HasPrefix(trimmed, key+" ") || strings.HasPrefix(trimmed, key+"=") {
			lines[i] = key + " = " + tomlValue(value)
			replaced = true
			break
		}
	}
	if replaced {
		return strings.Join(lines, "\n")
	}
	newLine := key + " = " + tomlValue(value)
	lines = append(lines[:insertAt], append([]string{newLine}, lines[insertAt:]...)...)
	return strings.Join(lines, "\n")
}

func upsertTomlTableValues(raw, table string, values map[string]any) string {
	lines := strings.Split(raw, "\n")
	header := "[" + table + "]"
	start := -1
	end := len(lines)
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.EqualFold(trimmed, header) {
			start = i
			continue
		}
		if start >= 0 && i > start && strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			end = i
			break
		}
	}
	if start < 0 {
		block := []string{"", header}
		for _, key := range orderedCodexProviderKeys() {
			block = append(block, key+" = "+tomlValue(values[key]))
		}
		return strings.TrimRight(raw, "\n") + strings.Join(block, "\n")
	}
	seen := map[string]bool{}
	for i := start + 1; i < end; i++ {
		trimmed := strings.TrimSpace(lines[i])
		for _, key := range orderedCodexProviderKeys() {
			if strings.HasPrefix(trimmed, key+" ") || strings.HasPrefix(trimmed, key+"=") {
				lines[i] = key + " = " + tomlValue(values[key])
				seen[key] = true
			}
		}
	}
	missing := make([]string, 0)
	for _, key := range orderedCodexProviderKeys() {
		if !seen[key] {
			missing = append(missing, key+" = "+tomlValue(values[key]))
		}
	}
	if len(missing) > 0 {
		lines = append(lines[:end], append(missing, lines[end:]...)...)
	}
	return strings.Join(lines, "\n")
}

func orderedCodexProviderKeys() []string {
	return []string{"name", "wire_api", "requires_openai_auth", "base_url"}
}

func tomlValue(value any) string {
	switch v := value.(type) {
	case bool:
		if v {
			return "true"
		}
		return "false"
	case string:
		body, _ := json.Marshal(v)
		return string(body)
	default:
		return fmt.Sprint(value)
	}
}
