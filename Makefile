# Makefile for GoplexCLI

VERSION ?= $(shell cat VERSION 2>/dev/null || echo "0.1.0")
LDFLAGS = -ldflags "-s -w -X main.version=$(VERSION)"

.PHONY: build install clean test run help

# Build the application
build:
	@echo "Building goplexcli v$(VERSION)..."
ifeq ($(OS),Windows_NT)
	@go build $(LDFLAGS) -o goplexcli.exe ./cmd/goplexcli
	@echo "Building preview helper..."
	@go build -o goplexcli-preview.exe ./cmd/preview
	@echo "Build complete: ./goplexcli.exe and ./goplexcli-preview.exe"
else
	@go build $(LDFLAGS) -o goplexcli ./cmd/goplexcli
	@echo "Building preview helper..."
	@go build -o goplexcli-preview ./cmd/preview
	@echo "Build complete: ./goplexcli and ./goplexcli-preview"
endif

# Build for all platforms
build-all:
	@echo "Building for all platforms..."
	@mkdir -p build
	@GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o build/goplexcli-darwin-amd64 ./cmd/goplexcli
	@GOOS=darwin GOARCH=amd64 go build -o build/goplexcli-preview-darwin-amd64 ./cmd/preview
	@GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o build/goplexcli-darwin-arm64 ./cmd/goplexcli
	@GOOS=darwin GOARCH=arm64 go build -o build/goplexcli-preview-darwin-arm64 ./cmd/preview
	@GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o build/goplexcli-linux-amd64 ./cmd/goplexcli
	@GOOS=linux GOARCH=amd64 go build -o build/goplexcli-preview-linux-amd64 ./cmd/preview
	@GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o build/goplexcli-linux-arm64 ./cmd/goplexcli
	@GOOS=linux GOARCH=arm64 go build -o build/goplexcli-preview-linux-arm64 ./cmd/preview
	@GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o build/goplexcli-windows-amd64.exe ./cmd/goplexcli
	@GOOS=windows GOARCH=amd64 go build -o build/goplexcli-preview-windows-amd64.exe ./cmd/preview
	@echo "Build complete: ./build/"

# Install to GOPATH/bin (cross-platform)
install:
	@echo "Installing goplexcli v$(VERSION) to GOPATH/bin..."
	@go install $(LDFLAGS) ./cmd/goplexcli/
	@go install ./cmd/preview/
	@echo "Installation complete"

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
ifeq ($(OS),Windows_NT)
	@cmd /c "if exist goplexcli.exe del /q goplexcli.exe"
	@cmd /c "if exist goplexcli-preview.exe del /q goplexcli-preview.exe"
	@cmd /c "if exist build rmdir /s /q build"
else
	@rm -f goplexcli goplexcli-preview
	@rm -rf build/
endif
	@echo "Clean complete"

# Run tests
test:
	@echo "Running tests..."
	@go test -v ./...

# Run the application
run: build
ifeq ($(OS),Windows_NT)
	@cmd /c goplexcli.exe
else
	@./goplexcli
endif

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
