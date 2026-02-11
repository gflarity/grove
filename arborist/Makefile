.PHONY: run build clean help test test-cover test-e2e lint vet deps

# Default target
help:
	@echo "Arborist - Grove Cluster Inspector - Available targets:"
	@echo "  make run          - Run the Arborist TUI"
	@echo "  make build        - Build the Arborist binary"
	@echo "  make test         - Run unit tests"
	@echo "  make test-cover   - Run unit tests with coverage report"
	@echo "  make test-e2e     - Run E2E tests (requires KUBECONFIG)"
	@echo "  make lint         - Run go vet"
	@echo "  make vet          - Run go vet"
	@echo "  make clean        - Clean build artifacts"
	@echo "  make deps         - Install dependencies"
	@echo "  make help         - Show this help"

# Run the TUI
run: build
	@echo "Running Arborist TUI..."
	@./bin/arborist

# Build binary
build:
	@echo "Building Arborist..."
	@mkdir -p bin
	@go build -o bin/arborist ./cmd/arborist
	@echo "Built: bin/arborist"

# Run unit tests
test:
	@echo "Running unit tests..."
	@go test -count=1 -timeout 60s ./internal/... 2>&1 | tee /tmp/arborist-test-output.txt

# Run unit tests with coverage report
test-cover:
	@echo "Running unit tests with coverage..."
	@go test -count=1 -timeout 60s -coverprofile=/tmp/arborist-coverage.out ./internal/... 2>&1 | tee /tmp/arborist-test-output.txt
	@go tool cover -func=/tmp/arborist-coverage.out | tail -1
	@echo "Full coverage report: go tool cover -html=/tmp/arborist-coverage.out"

# Run E2E tests against a real cluster.
# Requires KUBECONFIG to point at a running cluster with Grove operator deployed.
#
# For local development with a pre-existing cluster:
#   KUBECONFIG=~/.kube/config make test-e2e
#
# To run a specific E2E test:
#   KUBECONFIG=~/.kube/config go test -tags=e2e -timeout 15m -v ./internal/tui/... -run TestE2E_ForestView
test-e2e:
	@echo "Running E2E tests..."
	go test -tags=e2e -timeout 15m -v ./internal/tui/... -run TestE2E 2>&1 | tee /tmp/arborist-e2e-output.txt

# Run go vet
lint: vet

vet:
	@echo "Running go vet..."
	@go vet ./...

# Clean build artifacts
clean:
	@rm -rf bin/
	@echo "Cleaned build artifacts"

# Install dependencies
deps:
	@go mod download
	@GOFLAGS="-tags=e2e" go mod tidy
