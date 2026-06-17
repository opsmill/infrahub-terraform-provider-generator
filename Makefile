.PHONY: build test test-coverage lint fmt vet deps help

BUILD_DIR := bin
BINARY := generator

help: ## Display this help message
	@echo "Infrahub Terraform Provider Generator - Build System"
	@echo ""
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-15s %s\n", $$1, $$2}'

build: ## Build the generator binary
	@echo "Building $(BINARY)..."
	@mkdir -p $(BUILD_DIR)
	@go build -o $(BUILD_DIR)/$(BINARY) ./cmd/generator
	@echo "Binary available at $(BUILD_DIR)/$(BINARY)"

test: ## Run tests
	@echo "Running tests..."
	@go test ./...

test-coverage: ## Run tests with coverage
	@echo "Running tests with coverage..."
	@go test -coverprofile=coverage.out ./...
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

lint: ## Run golangci-lint
	@echo "Running linter..."
	@golangci-lint run ./...

fmt: ## Format Go code
	@echo "Formatting code..."
	@gofmt -w .

vet: ## Run go vet
	@echo "Running go vet..."
	@go vet ./...

deps: ## Download dependencies
	@echo "Downloading dependencies..."
	@go mod download
