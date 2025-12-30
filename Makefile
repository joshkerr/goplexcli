# Makefile for GoplexCLI

.PHONY: build install clean test run help

# Build the application
build:
	@echo "Building goplexcli..."
	@go build -o goplexcli ./cmd/goplexcli
	@echo "Build complete: ./goplexcli"

# Build for all platforms
build-all:
	@echo "Building for all platforms..."
	@GOOS=darwin GOARCH=amd64 go build -o build/goplexcli-darwin-amd64 ./cmd/goplexcli
	@GOOS=darwin GOARCH=arm64 go build -o build/goplexcli-darwin-arm64 ./cmd/goplexcli
	@GOOS=linux GOARCH=amd64 go build -o build/goplexcli-linux-amd64 ./cmd/goplexcli
	@GOOS=linux GOARCH=arm64 go build -o build/goplexcli-linux-arm64 ./cmd/goplexcli
	@GOOS=windows GOARCH=amd64 go build -o build/goplexcli-windows-amd64.exe ./cmd/goplexcli
	@echo "Build complete: ./build/"

# Install to /usr/local/bin (requires sudo on macOS/Linux)
install: build
	@echo "Installing goplexcli to /usr/local/bin..."
	@sudo mv goplexcli /usr/local/bin/
	@echo "Installation complete"

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	@rm -f goplexcli
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
	@echo "  make build       - Build the application"
	@echo "  make build-all   - Build for all platforms"
	@echo "  make install     - Install to /usr/local/bin"
	@echo "  make clean       - Remove build artifacts"
	@echo "  make test        - Run tests"
	@echo "  make run         - Build and run"
	@echo "  make deps        - Download and tidy dependencies"
	@echo "  make help        - Show this help message"
