package services

import (
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestBuildCodexConfigUpsertsProviderAndKeepsExistingTables(t *testing.T) {
	raw := `model_provider = "old"
model = "old-model"

[projects."/tmp/demo"]
trust_level = "trusted"
`
	out, err := buildCodexConfig(raw, "custom", "http://localhost:8787/v1", "gpt-5.5", "medium", true)
	if err != nil {
		t.Fatalf("buildCodexConfig: %v", err)
	}
	var decoded map[string]any
	if err := toml.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("generated toml is invalid: %v\n%s", err, out)
	}
	for _, want := range []string{
		`model_provider = "custom"`,
		`model = "gpt-5.5"`,
		`model_reasoning_effort = "medium"`,
		`[model_providers.custom]`,
		`base_url = "http://localhost:8787/v1"`,
		`[projects."/tmp/demo"]`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
