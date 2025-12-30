# Makefile for GoplexCLI

.PHONY: build install clean test run help

# Build the application
build:
	@echo "Building goplexcli..."
	@go build -o goplexcli ./cmd/goplexcli
	@echo "Building preview helper..."
	@go build -o goplexcli-preview ./cmd/preview
	@echo "Build complete: ./goplexcli and ./goplexcli-preview"

# Build for all platforms
build-all:
	@echo "Building for all platforms..."
	@mkdir -p build
	@GOOS=darwin GOARCH=amd64 go build -o build/goplexcli-darwin-amd64 ./cmd/goplexcli
	@GOOS=darwin GOARCH=amd64 go build -o build/goplexcli-preview-darwin-amd64 ./cmd/preview
	@GOOS=darwin GOARCH=arm64 go build -o build/goplexcli-darwin-arm64 ./cmd/goplexcli
	@GOOS=darwin GOARCH=arm64 go build -o build/goplexcli-preview-darwin-arm64 ./cmd/preview
	@GOOS=linux GOARCH=amd64 go build -o build/goplexcli-linux-amd64 ./cmd/goplexcli
	@GOOS=linux GOARCH=amd64 go build -o build/goplexcli-preview-linux-amd64 ./cmd/preview
	@GOOS=linux GOARCH=arm64 go build -o build/goplexcli-linux-arm64 ./cmd/goplexcli
	@GOOS=linux GOARCH=arm64 go build -o build/goplexcli-preview-linux-arm64 ./cmd/preview
	@GOOS=windows GOARCH=amd64 go build -o build/goplexcli-windows-amd64.exe ./cmd/goplexcli
	@GOOS=windows GOARCH=amd64 go build -o build/goplexcli-preview-windows-amd64.exe ./cmd/preview
	@echo "Build complete: ./build/"

# Install to /usr/local/bin (requires sudo on macOS/Linux)
install: build
	@echo "Installing goplexcli to /usr/local/bin..."
	@sudo mv goplexcli /usr/local/bin/
	@sudo mv goplexcli-preview /usr/local/bin/
	@echo "Installation complete"

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	@rm -f goplexcli goplexcli-preview
	@rm -rf build/
	@echo "Clean complete"

# Run tests
test:
	@echo "Running tests..."
	@go test -v ./...

# Run the application
run: build
	@./goplexcli

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	@go mod download
	@go mod tidy
	@echo "Dependencies updated"

# Show help
help:
	@echo "GoplexCLI Makefile"
	@echo ""
	@echo "Usage:"
	@echo "  make build       - Build the application and preview helper"
	@echo "  make build-all   - Build for all platforms"
	@echo "  make install     - Install to /usr/local/bin"
	@echo "  make clean       - Remove build artifacts"
	@echo "  make test        - Run tests"
	@echo "  make run         - Build and run"
	@echo "  make deps        - Download and tidy dependencies"
	@echo "  make help        - Show this help message"
