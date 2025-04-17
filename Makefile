# =============================================================================
# 🔄 Load Environment & Configs
# =============================================================================

# Suppress the output of the commands
MAKEFLAGS += --no-print-directory

# Load .env file if it exists
ifneq (,$(wildcard .env))
    include .env
    # Only export specific variables that need to be in the environment
    export GOOS
    export GOARCH
    export CGO_ENABLED
    export PROJECT_NAME
    export ORGANIZATION
    export DATABASE_URL
    # Add other variables that actually need to be in the environment
endif

# =============================================================================
# 🎯 Project Configuration
# =============================================================================
# Project Settings
PROJECT_NAME ?= qtap
BINARY_NAME ?= qtap
ORGANIZATION ?= qpoint-io
DESCRIPTION ?= "🧬 Qtap: An eBPF agent that captures pre-encrypted network traffic, providing rich context about egress connections and their originating processes."
MAINTAINER ?= "Qpoint Team \<hello@qpoint.io\>"

VERSION=$${GIT_VERSION:-$$(git describe --tags --always --dirty)}
GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
GIT_BRANCH ?= $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "unknown")
BUILD_TIME ?= $(shell date -u '+%Y-%m-%d_%H:%M:%S')

# Go Configuration
GO ?= go
GOCMD = $(shell which go)
GOPATH ?= $(shell $(GO) env GOPATH)
GOBIN ?= $(GOPATH)/bin
GOOS ?= $(shell $(GO) env GOOS)
GOARCH ?= $(shell $(GO) env GOARCH)
CGO_ENABLED ?= 0

# Tools & Linters
GOLANGCI_LINT ?= $(GO) tool github.com/golangci/golangci-lint/cmd/golangci-lint
GOVULNCHECK ?= $(GO) tool golang.org/x/vuln/cmd/govulncheck
GOTESTSUM ?= $(GO) tool gotest.tools/gotestsum
MOCKGEN ?= $(GO) tool go.uber.org/mock/mockgen

# Directories
ROOT_DIR ?= $(shell pwd)
BIN_DIR ?= $(ROOT_DIR)/bin
BPF_DIR ?= $(ROOT_DIR)/bpf
DIST_DIR ?= $(ROOT_DIR)/dist

# Source Files
GOFILES = $(shell find . -type f -name '*.go' -not -path "./vendor/*" -not -path "./.git/*")
GOPACKAGES = $(shell $(GO) list ./... | grep -v /vendor/)

# Build Configuration
BUILD_TAGS ?= 
EXTRA_TAGS ?=
ALL_TAGS = $(BUILD_TAGS) $(EXTRA_TAGS)

# Linker Flags
LD_FLAGS += -s -w
LD_FLAGS += -X '$(shell go list -m)/pkg/buildinfo.version=$(VERSION)'
LD_FLAGS += -X '$(shell go list -m)/pkg/buildinfo.commit=$(GIT_COMMIT)'
LD_FLAGS += -X '$(shell go list -m)/pkg/buildinfo.branch=$(GIT_BRANCH)'
LD_FLAGS += -X '$(shell go list -m)/pkg/buildinfo.buildTime=$(BUILD_TIME)'

# Performance & Debug Flags
GCFLAGS ?=
ASMFLAGS ?=

# Test Configuration
TEST_TIMEOUT ?= 5m
TEST_FLAGS ?= -v -race
BENCH_FLAGS ?= -benchmem
BENCH_TIME ?= 2s
TEST_PATTERN ?= .
SKIP_PATTERN ?=

# Cross-Compilation Targets
PLATFORMS ?= \
    linux/amd64/- \
    linux/arm64/-

# =============================================================================
# 🎨 Terminal Colors & Emoji
# =============================================================================
# Colors
BLUE := \033[34m
GREEN := \033[32m
YELLOW := \033[33m
RED := \033[31m
MAGENTA := \033[35m
CYAN := \033[36m
WHITE := \033[37m
BOLD := \033[1m
RST := \033[0m

# Status Indicators - define these as functions, not variables
# Status Indicators
INFO := $(shell printf "$(BLUE)ℹ️ ")
SUCCESS := $(shell printf "$(GREEN)✅ ")
WARN := $(shell printf "$(YELLOW)⚠️  ")
ERROR := $(shell printf "$(RED)❌ ")
WORKING := $(shell printf "$(CYAN)🔨 ")
DEBUG := $(shell printf "$(MAGENTA)🔍 ")
ROCKET := $(shell printf "$(GREEN)🚀 ")
PACKAGE := $(shell printf "$(CYAN)📦 ")
TRASH := $(shell printf "$(YELLOW)🗑️  ")
RESET := $(shell printf "$(RST)")

# =============================================================================
# 🎯 Core Build System
# =============================================================================
.PHONY: build
build: $(BIN_DIR) generate ## Build for the current platform
	@echo $(INFO) Building $(BINARY_NAME)... $(RESET)
	CGO_ENABLED=$(CGO_ENABLED) \
	$(GO) build -tags '$(ALL_TAGS)' \
		-ldflags '$(LD_FLAGS)' \
		-gcflags '$(GCFLAGS)' \
		-asmflags '$(ASMFLAGS)' \
		-o $(BIN_DIR)/$(notdir $(BINARY_NAME))  \
		cmd/$(BINARY_NAME)/main.go
	@echo $(SUCCESS) Build complete! $(RESET)

# =============================================================================
# 🔄 Development Workflow
# =============================================================================
.PHONY: run
run: build ## Run the application
	@echo $(ROCKET) Running $(PROJECT_NAME)... $(RESET)
	./bin/$(BINARY_NAME) --log-level=debug --log-encoding=console

.PHONY: run-config
run-config: build ## Run the application with a specific config
	@echo $(ROCKET) Running $(PROJECT_NAME) with config... $(RESET)
	./bin/$(BINARY_NAME) --log-level=debug --log-encoding=console --config=$$(find ./examples -type f -name "*.yaml" | go tool gum filter --prompt="> " --indicator=">" --placeholder="Select a config file..." --header="Select a config file to run")

.PHONY: generate
generate: ## Run code generation
	@echo $(WORKING) Running code generation... $(RESET)
	$(GO) generate ./...
	@echo $(SUCCESS) Generation complete! $(RESET)

# =============================================================================
# 🧪 Testing & Quality
# =============================================================================
.PHONY: test
test: ## Run tests
	@echo $(INFO) Running tests... $(RESET)
	$(GOTESTSUM) --format pkgname --hide-summary=skipped --format-hide-empty-pkg --format-icons=hivis -- \
		-timeout $(TEST_TIMEOUT) \
		-run '$(TEST_PATTERN)' \
		$(if $(SKIP_PATTERN),-skip '$(SKIP_PATTERN)') \
		./...

.PHONY: lint
lint: ## Run linters
	@echo $(INFO) Running linters... $(RESET)
	$(GOLANGCI_LINT) run  --config ./.golangci.yaml --fix
	@echo $(SUCCESS) Lint complete! $(RESET)

.PHONY: fmt
fmt: ## Format code
	@echo $(INFO) Formatting Go code... $(RESET)
	$(GO) fmt ./...
	@echo $(SUCCESS) Format Go complete! $(RESET)
	@echo $(INFO) Formatting BPF code... $(RESET)
	@find ./bpf -type f -not -path "./bpf/headers/*" -name "*.[ch]" | xargs clang-format -i --Werror
	@echo $(SUCCESS) Format BPF complete! $(RESET)

.PHONY: vet
vet: ## Run go vet
	@echo $(INFO) Running go vet... $(RESET)
	$(GO) vet ./...
	@echo $(SUCCESS) Vet complete! $(RESET)

.PHONY: security
security: ## Run security checks
	@echo $(INFO) Running security checks... $(RESET)
	$(GOVULNCHECK) ./...
	@echo $(SUCCESS) Security check complete! $(RESET)

# =============================================================================
# 🏗️ Build Variations
# =============================================================================
.PHONY: build-all
build-all: $(DIST_DIR) ## Build for all platforms
	@echo $(WORKING) Building for all platforms... $(RESET)
	@$(foreach platform,$(PLATFORMS),\
		$(eval OS := $(word 1,$(subst /, ,$(platform)))) \
		$(eval ARCH := $(word 2,$(subst /, ,$(platform)))) \
		$(eval ARM := $(word 3,$(subst /, ,$(platform)))) \
		echo $(WORKING) Building for $(OS) $(ARCH)... && \
		GOOS=$(OS) GOARCH=$(ARCH) $(if $(ARM),GOARM=$(ARM)) \
		CGO_ENABLED=$(CGO_ENABLED) \
		$(GO) build -tags '$(ALL_TAGS)' \
			-ldflags '$(LD_FLAGS)' \
			-o $(DIST_DIR)/$(BINARY_NAME)-$(OS)-$(ARCH) \
			./cmd/$(BINARY_NAME)/main.go ; \
	)
	@echo $(PACKAGE) Creating release archives... $(RESET)
	@cd $(DIST_DIR) && \
	for file in $(BINARY_NAME)-* ; do \
		if [ -f "$$file" ] && [ "$$file" != *.tar.gz ]; then \
			tar czf "$$file-$(VERSION).tar.gz" "$$file" || exit 1; \
			rm -f "$$file"; \
		fi \
	done
	@echo $(SUCCESS) All platforms built and archived! $(RESET)

.PHONY: build-debug
build-debug: GCFLAGS += -N -l ## Build with debug symbols
build-debug: BUILD_TAGS += debug
build-debug: build

.PHONY: build-race
build-race: CGO_ENABLED=1 ## Build with race detector
build-race: BUILD_TAGS += race
build-race: build

# =============================================================================
# 🔄 CI/CD Integration
# =============================================================================
.PHONY: ci
ci: ## Run CI pipeline
	@echo $(INFO) Running CI pipeline... $(RESET)
	$(MAKE) deps-verify
	$(MAKE) lint
	$(MAKE) security
	if [ -n "$$(git status --porcelain)" ]; then \
		if [ "$${ENV}" = "dev" ]; then \
			echo $(WARN) Working tree is not clean. This will fail CI checks. $(RESET); \
		else \
			git status; \
			git --no-pager diff; \
			echo 'Working tree is not clean, did you forget to run "make fmt" or "make generate"?'; \
			exit 1; \
		fi \
	fi
	@echo $(SUCCESS) CI pipeline complete! $(RESET)

# =============================================================================
# 🧹 Cleanup & Maintenance
# =============================================================================
.PHONY: clean
clean: ## Clean build artifacts
	@echo $(TRASH) Cleaning build artifacts... $(RESET)
	rm -rf $(BIN_DIR) $(DIST_DIR)
	$(GO) clean -cache -testcache -modcache
	@echo $(SUCCESS) Clean complete! $(RESET)

.PHONY: deps
deps: ## Install dependencies
	@echo $(WORKING) Installing dependencies... $(RESET)
	$(GO) mod download
	@echo $(SUCCESS) Dependencies installed! $(RESET)

.PHONY: deps-update
deps-update: ## Update dependencies
	@echo $(WORKING) Updating dependencies... $(RESET)
	$(GO) get -u ./...
	$(GO) mod tidy
	@echo $(SUCCESS) Dependencies updated! $(RESET)

.PHONY: deps-verify
deps-verify: ## Verify dependencies
	@echo $(INFO) Verifying dependencies... $(RESET)
	$(GO) mod verify
	@echo $(SUCCESS) Dependencies verified! $(RESET)

# =============================================================================
# 🛠️ Tools & Utilities
# =============================================================================

.PHONY: version
version: ## Display version information
	@echo "$(CYAN)Version:$(RST)    $(VERSION)"
	@echo "$(CYAN)Commit:$(RST)     $(GIT_COMMIT)"
	@echo "$(CYAN)Branch:$(RST)     $(GIT_BRANCH)"
	@echo "$(CYAN)Built:$(RST)      $(BUILD_TIME)"
	@echo "$(CYAN)Go version:$(RST) $(shell $(GO) version)"

# =============================================================================
# 📁 Directory Creation
# =============================================================================
$(BIN_DIR) $(DIST_DIR) $(DOCS_DIR):
	mkdir -p $@

# =============================================================================
# 💡 Help
# =============================================================================
.PHONY: help
help: ## Show this help message
	@echo "$(CYAN)$(BOLD) $(DESCRIPTION)$(RST)"
	@echo "$(WHITE)Maintained by $(MAINTAINER)$(RST)\n"
	@echo "$(CYAN)$(BOLD)Available targets:$(RST)"
	@awk 'BEGIN {FS = ":.*##"; printf ""} \
		/^[a-zA-Z_-]+:.*?##/ { printf "  $(CYAN)%-20s$(RST) %s\n", $$1, $$2 } \
		/^##@/ { printf "\n$(MAGENTA)%s$(RST)\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

.DEFAULT_GOAL := help