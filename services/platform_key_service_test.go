package services

import (
	"testing"

	"github.com/wfu-work/free-ai-go/domains"
)

func TestPlatformKeyModelMappingAllowed(t *testing.T) {
	key := domains.PlatformKey{AllowedModels: `["group:fast","provider:openai","gpt-public"]`}
	model := domains.ModelMapping{
		PublicModel:  "gpt-4o",
		Provider:     "freemodel",
		AccountGroup: "fast",
	}
	if !PlatformKeyServiceApp.ModelMappingAllowed(key, model) {
		t.Fatal("expected group rule to allow model")
	}

	model.AccountGroup = "slow"
	if !PlatformKeyServiceApp.ModelMappingAllowed(key, domains.ModelMapping{PublicModel: "anything", Provider: "openai"}) {
		t.Fatal("expected provider rule to allow model")
	}
	if !PlatformKeyServiceApp.ModelMappingAllowed(key, domains.ModelMapping{PublicModel: "gpt-public"}) {
		t.Fatal("expected public model rule to allow model")
	}
	if PlatformKeyServiceApp.ModelMappingAllowed(key, model) {
		t.Fatal("unexpected model permission")
	}
}

func TestNormalizePlatformKeyAccountGroupFilterKeepsEmpty(t *testing.T) {
	if got := normalizePlatformKeyAccountGroupFilter(" "); got != "" {
		t.Fatalf("expected empty account group filter, got %q", got)
	}
	if got := normalizePlatformKeyAccountGroupFilter(" default "); got != "default" {
		t.Fatalf("expected explicit default account group filter, got %q", got)
	}
}
