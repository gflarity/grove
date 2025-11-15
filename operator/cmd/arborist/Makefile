.PHONY: run build clean help

# Default target
help:
	@echo "Forest - Grove Operator TUI - Available targets:"
	@echo "  make run          - Run the Forest TUI"
	@echo "  make build        - Build the Forest binary"
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

# Clean build artifacts
clean:
	@rm -f forest
	@echo "Cleaned build artifacts"

# Install dependencies
deps:
	@go mod download
	@go mod tidy