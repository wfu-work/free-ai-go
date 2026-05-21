APP ?= freeai
BIN_DIR ?= bin

GO ?= go
GOOS ?= $(shell $(GO) env GOOS)
GOARCH ?= $(shell $(GO) env GOARCH)
CGO_ENABLED ?=

GO_PACKAGES := ./...
GO_FILES := $(shell find . -path './.git' -prune -o -path './.cache' -prune -o -path './$(BIN_DIR)' -prune -o -name '*.go' -print)
OS_ARCH := $(GOOS)-$(GOARCH)
EXE_EXT := $(if $(filter windows,$(GOOS)),.exe,)
TARGET_DIR := $(BIN_DIR)/$(OS_ARCH)
TARGET := $(TARGET_DIR)/$(APP)$(EXE_EXT)
PLATFORMS ?= darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS ?= -s -w

GO_ENV :=
ifneq ($(strip $(CGO_ENABLED)),)
GO_ENV += CGO_ENABLED=$(CGO_ENABLED)
endif

.DEFAULT_GOAL := help

.PHONY: help all check build build-all run test vet fmt tidy clean print-vars

help: ## Show available targets
	@awk 'BEGIN {FS = ":.*##"; printf "Usage: make <target>\n\nTargets:\n"} /^[a-zA-Z0-9_.-]+:.*##/ {printf "  %-12s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

all: test build ## Run tests and build the binary

check: fmt vet test ## Format, vet, and test

build: ## Build the service binary
	@mkdir -p $(TARGET_DIR)
	$(GO_ENV) GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(TARGET) .

build-all: ## Build binaries for common platforms
	@set -e; \
	for platform in $(PLATFORMS); do \
		os=$${platform%/*}; \
		arch=$${platform#*/}; \
		ext=""; \
		if [ "$$os" = "windows" ]; then ext=".exe"; fi; \
		out="$(BIN_DIR)/$$os-$$arch/$(APP)$$ext"; \
		mkdir -p "$$(dirname "$$out")"; \
		echo "building $$out"; \
		$(GO_ENV) GOOS=$$os GOARCH=$$arch $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o "$$out" .; \
	done

run: ## Run the service locally
	$(GO_ENV) $(GO) run .

test: ## Run all Go tests
	$(GO_ENV) $(GO) test $(GO_PACKAGES)

vet: ## Run go vet
	$(GO_ENV) $(GO) vet $(GO_PACKAGES)

fmt: ## Format Go source files
	gofmt -w $(GO_FILES)

tidy: ## Tidy Go module files
	$(GO_ENV) $(GO) mod tidy

clean: ## Remove build artifacts
	rm -rf $(BIN_DIR)

print-vars: ## Print build variables
	@printf 'APP=%s\n' '$(APP)'
	@printf 'OS_ARCH=%s\n' '$(OS_ARCH)'
	@printf 'TARGET_DIR=%s\n' '$(TARGET_DIR)'
	@printf 'TARGET=%s\n' '$(TARGET)'
	@printf 'GOOS=%s\n' '$(GOOS)'
	@printf 'GOARCH=%s\n' '$(GOARCH)'
	@printf 'PLATFORMS=%s\n' '$(PLATFORMS)'
	@printf 'VERSION=%s\n' '$(VERSION)'
	@printf 'COMMIT=%s\n' '$(COMMIT)'
	@printf 'BUILD_TIME=%s\n' '$(BUILD_TIME)'
