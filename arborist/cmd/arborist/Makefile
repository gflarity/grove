.PHONY: run build clean help test test-e2e

# Default target
help:
	@echo "Forest - Grove Operator TUI - Available targets:"
	@echo "  make run          - Run the Forest TUI"
	@echo "  make build        - Build the Forest binary"
	@echo "  make test         - Run unit tests"
	@echo "  make test-e2e     - Run E2E tests (requires KUBECONFIG)"
	@echo "  make clean        - Clean build artifacts"
	@echo "  make deps         - Install dependencies"
	@echo "  make help         - Show this help"

# Run the TUI
run:
	@echo "Running Forest TUI..."
	@go run main.go

# Build binary
build:
	@echo "Building Forest..."
	@go build -o forest main.go
	@echo "Built: forest"

# Run unit tests
test:
	@echo "Running unit tests..."
	@go test -count=1 -timeout 60s ./... 2>&1 | tee /tmp/arborist-test-output.txt

# Run E2E tests against a real cluster.
# Requires KUBECONFIG to point at a running cluster with Grove operator deployed.
#
# For local development with a pre-existing cluster:
#   KUBECONFIG=~/.kube/config make test-e2e
#
# To run a specific E2E test:
#   KUBECONFIG=~/.kube/config go test -tags=e2e -timeout 15m -v ./... -run TestE2E_ForestView
test-e2e:
	@echo "Running E2E tests..."
	go test -tags=e2e -timeout 15m -v ./... -run TestE2E 2>&1 | tee /tmp/arborist-e2e-output.txt

# Clean build artifacts
clean:
	@rm -f forest
	@echo "Cleaned build artifacts"

# Install dependencies
deps:
	@go mod download
	@GOFLAGS="-tags=e2e" go mod tidy