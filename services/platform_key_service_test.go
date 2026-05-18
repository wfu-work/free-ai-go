package services

import (
	"testing"

	"freeai/domains"
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
