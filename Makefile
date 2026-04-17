# Makefile for GoplexCLI

ifeq ($(OS),Windows_NT)
VERSION ?= $(shell type VERSION 2>nul || echo 0.1.0)
else
VERSION ?= $(shell cat VERSION 2>/dev/null || echo 0.1.0)
endif
LDFLAGS = -ldflags "-s -w -X main.version=$(VERSION)"

# Termux (Android) support. The packaged Go is -trimpath'd so GOROOT must be
# set explicitly; Android has no upstream toolchain tarball so auto-download
# must be disabled; and some Termux Go builds have an os.Args off-by-one bug
# where argv[0] is dropped, causing every subcommand to mis-dispatch. Probe
# for the bug and prepend a throwaway arg if needed.
ifeq ($(shell uname -o 2>/dev/null),Android)
export GOROOT ?= $(shell readlink -f $$(command -v go) 2>/dev/null | sed 's|/bin/go$$||')
export GOTOOLCHAIN ?= local
export CGO_ENABLED ?= 0
ifeq ($(shell go version 2>/dev/null | grep -c '^go version'),0)
GO := go _shim_
else
GO := go
endif
else
GO ?= go
endif

.PHONY: build install clean test run help lint vet build-all deps

# Build the application
build:
	@echo "Building goplexcli v$(VERSION)..."
ifeq ($(OS),Windows_NT)
	@$(GO) build $(LDFLAGS) -o goplexcli.exe ./cmd/goplexcli
	@echo "Building preview helper..."
	@$(GO) build -o goplexcli-preview.exe ./cmd/preview
	@echo "Build complete: ./goplexcli.exe and ./goplexcli-preview.exe"
else
	@$(GO) build $(LDFLAGS) -o goplexcli ./cmd/goplexcli
	@echo "Building preview helper..."
	@$(GO) build -o goplexcli-preview ./cmd/preview
	@echo "Build complete: ./goplexcli and ./goplexcli-preview"
endif

# Build for all platforms
build-all:
	@echo "Building for all platforms..."
	@mkdir -p build
	@GOOS=darwin GOARCH=amd64 $(GO) build $(LDFLAGS) -o build/goplexcli-darwin-amd64 ./cmd/goplexcli
	@GOOS=darwin GOARCH=amd64 $(GO) build -o build/goplexcli-preview-darwin-amd64 ./cmd/preview
	@GOOS=darwin GOARCH=arm64 $(GO) build $(LDFLAGS) -o build/goplexcli-darwin-arm64 ./cmd/goplexcli
	@GOOS=darwin GOARCH=arm64 $(GO) build -o build/goplexcli-preview-darwin-arm64 ./cmd/preview
	@GOOS=linux GOARCH=amd64 $(GO) build $(LDFLAGS) -o build/goplexcli-linux-amd64 ./cmd/goplexcli
	@GOOS=linux GOARCH=amd64 $(GO) build -o build/goplexcli-preview-linux-amd64 ./cmd/preview
	@GOOS=linux GOARCH=arm64 $(GO) build $(LDFLAGS) -o build/goplexcli-linux-arm64 ./cmd/goplexcli
	@GOOS=linux GOARCH=arm64 $(GO) build -o build/goplexcli-preview-linux-arm64 ./cmd/preview
	@GOOS=windows GOARCH=amd64 $(GO) build $(LDFLAGS) -o build/goplexcli-windows-amd64.exe ./cmd/goplexcli
	@GOOS=windows GOARCH=amd64 $(GO) build -o build/goplexcli-preview-windows-amd64.exe ./cmd/preview
	@GOOS=android GOARCH=arm64 $(GO) build $(LDFLAGS) -o build/goplexcli-android-arm64 ./cmd/goplexcli
	@GOOS=android GOARCH=arm64 $(GO) build -o build/goplexcli-preview-android-arm64 ./cmd/preview
	@echo "Build complete: ./build/"

# Install to GOPATH/bin (cross-platform)
install:
	@echo "Installing goplexcli v$(VERSION) to GOPATH/bin..."
	@$(GO) install $(LDFLAGS) ./cmd/goplexcli/
	@$(GO) install ./cmd/preview/
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
	@$(GO) test -v ./...

# Run linter
lint:
	@echo "Running linter..."
	@golangci-lint run ./...

# Run go vet
vet:
	@echo "Running go vet..."
	@$(GO) vet ./...

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
	@$(GO) mod download
	@$(GO) mod tidy
	@echo "Dependencies updated"

# Show help
help:
	@echo "GoplexCLI Makefile"
	@echo ""
	@echo "Usage:"
	@echo "  make build       - Build the application and preview helper"
	@echo "  make build-all   - Build for all platforms"
	@echo "  make install     - Install to GOPATH/bin"
	@echo "  make clean       - Remove build artifacts"
	@echo "  make test        - Run tests"
	@echo "  make lint        - Run golangci-lint"
	@echo "  make vet         - Run go vet"
	@echo "  make run         - Build and run"
	@echo "  make deps        - Download and tidy dependencies"
	@echo "  make help        - Show this help message"
