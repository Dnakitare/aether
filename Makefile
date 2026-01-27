.PHONY: help build test lint clean install tools dev integration-test coverage run

# Variables
BINARY_NAME=aether
BIN_DIR=bin
COVERAGE_FILE=coverage.txt
COVERAGE_HTML=coverage.html

# Go commands
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOMOD=$(GOCMD) mod
GOGET=$(GOCMD) get
GOCLEAN=$(GOCMD) clean
GOINSTALL=$(GOCMD) install

# Build flags
LDFLAGS=-ldflags "-s -w"
BUILD_FLAGS=-trimpath $(LDFLAGS)

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build the Aether binary
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BIN_DIR)
	$(GOBUILD) $(BUILD_FLAGS) -o $(BIN_DIR)/$(BINARY_NAME) ./cmd/aether
	@echo "Build complete: $(BIN_DIR)/$(BINARY_NAME)"

build-debug: ## Build with debug symbols
	@echo "Building $(BINARY_NAME) with debug symbols..."
	@mkdir -p $(BIN_DIR)
	$(GOBUILD) -o $(BIN_DIR)/$(BINARY_NAME) ./cmd/aether
	@echo "Build complete: $(BIN_DIR)/$(BINARY_NAME)"

install: build ## Install the binary to $GOPATH/bin
	@echo "Installing $(BINARY_NAME)..."
	$(GOINSTALL) ./cmd/aether
	@echo "Installed to $(shell go env GOPATH)/bin/$(BINARY_NAME)"

test: ## Run unit tests
	@echo "Running unit tests..."
	$(GOTEST) -v -race -short ./...

test-verbose: ## Run unit tests with verbose output
	@echo "Running unit tests (verbose)..."
	$(GOTEST) -v -race -short -count=1 ./...

coverage: ## Generate test coverage report
	@echo "Generating coverage report..."
	$(GOTEST) -race -coverprofile=$(COVERAGE_FILE) -covermode=atomic ./...
	$(GOCMD) tool cover -html=$(COVERAGE_FILE) -o $(COVERAGE_HTML)
	@echo "Coverage report generated: $(COVERAGE_HTML)"
	@$(GOCMD) tool cover -func=$(COVERAGE_FILE) | grep total

integration-test: ## Run integration tests
	@echo "Running integration tests..."
	$(GOTEST) -v -race -run Integration ./tests/integration/...

lint: ## Run linters
	@echo "Running linters..."
	@which golangci-lint > /dev/null || (echo "golangci-lint not installed. Run 'make tools' first." && exit 1)
	golangci-lint run --timeout=5m ./...

fmt: ## Format code with gofmt
	@echo "Formatting code..."
	gofmt -s -w .

fmt-check: ## Check if code is formatted
	@echo "Checking code formatting..."
	@if [ -n "$$(gofmt -l .)" ]; then \
		echo "Code is not formatted. Run 'make fmt'"; \
		gofmt -l .; \
		exit 1; \
	fi

vet: ## Run go vet
	@echo "Running go vet..."
	$(GOCMD) vet ./...

tidy: ## Tidy and verify go.mod
	@echo "Tidying go.mod..."
	$(GOMOD) tidy
	$(GOMOD) verify

clean: ## Clean build artifacts
	@echo "Cleaning..."
	$(GOCLEAN)
	rm -rf $(BIN_DIR)
	rm -f $(COVERAGE_FILE) $(COVERAGE_HTML)
	@echo "Clean complete"

tools: ## Install development tools
	@echo "Installing development tools..."
	$(GOINSTALL) github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@echo "Tools installed"

dev: ## Start development environment with Docker Compose
	@echo "Starting development environment..."
	docker-compose -f deployments/docker/docker-compose.dev.yml up -d
	@echo "Development environment started"

dev-down: ## Stop development environment
	@echo "Stopping development environment..."
	docker-compose -f deployments/docker/docker-compose.dev.yml down
	@echo "Development environment stopped"

run: build ## Build and run the daemon
	@echo "Running Aether daemon..."
	./$(BIN_DIR)/$(BINARY_NAME) daemon

run-debug: build-debug ## Build and run with debug logging
	@echo "Running Aether daemon (debug mode)..."
	./$(BIN_DIR)/$(BINARY_NAME) --log-level=debug daemon

verify: fmt-check vet lint test ## Run all verification checks

ci: verify coverage ## Run all CI checks (formatting, linting, tests, coverage)

all: clean tools build test lint ## Clean, install tools, build, test, and lint

.DEFAULT_GOAL := help
