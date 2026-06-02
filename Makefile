# Makefile for GoplexCLI

# Read the version from the VERSION file. On Windows, Make is 4.x so $(file)
# works and is shell-independent — it resolves from PowerShell/cmd AND Git-Bash.
# macOS ships Make 3.81 (no $(file)), so it falls back to a shell `cat`.
ifeq ($(OS),Windows_NT)
VERSION ?= $(or $(strip $(file <VERSION)),0.1.0)
else
VERSION ?= $(shell cat VERSION 2>/dev/null || echo 0.1.0)
endif
LDFLAGS = -ldflags "-s -w -X main.version=$(VERSION)"

# GitHub repository used by the release flow.
REPO = joshkerr/goplexcli

# Termux (Android) support. The packaged Go is -trimpath'd so GOROOT must be
# set explicitly; Android has no upstream toolchain tarball so auto-download
# must be disabled; and some Termux Go builds have an os.Args off-by-one bug
# where argv[0] is dropped, causing every subcommand to mis-dispatch. Probe
# for the bug and prepend a throwaway arg if needed.
ifeq ($(OS),Windows_NT)
# Windows is never Termux; skip the uname probe, which errors under cmd.exe
# ("The system cannot find the path specified.") on every invocation.
GO ?= go
else
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
endif

.PHONY: build install clean test run help lint vet build-all deps bump release-preflight release

# Running `make` with no target shows the help menu instead of building.
.DEFAULT_GOAL := help

# Build the application
build:
	@echo "Building goplexcli v$(VERSION)..."
ifeq ($(OS),Windows_NT)
	@$(GO) build $(LDFLAGS) -o goplexcli.exe ./cmd/goplexcli
	@echo "Build complete: ./goplexcli.exe"
else
	@$(GO) build $(LDFLAGS) -o goplexcli ./cmd/goplexcli
	@echo "Build complete: ./goplexcli"
endif

# Build for all platforms
build-all:
	@echo "Building for all platforms..."
	@mkdir -p build
	@GOOS=darwin GOARCH=amd64 $(GO) build $(LDFLAGS) -o build/goplexcli-darwin-amd64 ./cmd/goplexcli
	@GOOS=darwin GOARCH=arm64 $(GO) build $(LDFLAGS) -o build/goplexcli-darwin-arm64 ./cmd/goplexcli
	@GOOS=linux GOARCH=amd64 $(GO) build $(LDFLAGS) -o build/goplexcli-linux-amd64 ./cmd/goplexcli
	@GOOS=linux GOARCH=arm64 $(GO) build $(LDFLAGS) -o build/goplexcli-linux-arm64 ./cmd/goplexcli
	@GOOS=windows GOARCH=amd64 $(GO) build $(LDFLAGS) -o build/goplexcli-windows-amd64.exe ./cmd/goplexcli
	@GOOS=android GOARCH=arm64 $(GO) build $(LDFLAGS) -o build/goplexcli-android-arm64 ./cmd/goplexcli
	@echo "Build complete: ./build/"

# Install to GOPATH/bin (cross-platform)
install:
	@echo "Installing goplexcli v$(VERSION) to GOPATH/bin..."
	@$(GO) install $(LDFLAGS) ./cmd/goplexcli/
	@echo "Installation complete"

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
ifeq ($(OS),Windows_NT)
	@cmd /c "if exist goplexcli.exe del /q goplexcli.exe"
	@cmd /c "if exist build rmdir /s /q build"
else
	@rm -f goplexcli
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

# --- Release ---------------------------------------------------------------
# Release flow (mirrors .github/workflows/release.yml, which triggers on a
# pushed v* tag and uploads the per-platform binaries used by 'goplexcli
# update'):
#
#   make bump V=0.3.0     # write VERSION, commit, push the current branch
#   make release          # run checks, tag v$(VERSION), push the tag
#
# Recipes below use only plain commands (no &&, ||, if/then, or redirects) so
# they run whether Make's shell is cmd.exe (PowerShell) or sh (Git-Bash/macOS).

# Bump the VERSION file, commit, and push. Usage: make bump V=X.Y.Z
bump:
	$(if $(V),,$(error Usage: make bump V=X.Y.Z))
	@echo $(V)>VERSION
	@git add VERSION
	@git commit -m "chore: bump version to $(V)"
	@git push origin HEAD
	@echo Bumped to $(V) and pushed. Run make release to tag and publish.

# Verify the working tree is clean. git diff exits non-zero on changes, which
# stops make; untracked gitignored build artifacts are not considered.
release-preflight:
	@echo Checking working tree is clean...
	git diff --quiet
	git diff --cached --quiet

# Tag the current VERSION and push the tag to trigger the release workflow.
# Gated on vet + tests so a broken build is never tagged. `git tag` fails if the
# tag already exists, which stops the release.
release: release-preflight vet test
	@echo Tagging and pushing v$(VERSION)...
	git tag -a v$(VERSION) -m "Release v$(VERSION)"
	git push origin v$(VERSION)
	@echo Pushed v$(VERSION). CI: https://github.com/$(REPO)/actions/workflows/release.yml

# Show help
help:
	@echo "GoplexCLI Makefile  (current version: v$(VERSION))"
	@echo ""
	@echo "Usage:"
	@echo "  make build       - Build the application"
	@echo "  make build-all   - Build for all platforms"
	@echo "  make install     - Install to GOPATH/bin"
	@echo "  make clean       - Remove build artifacts"
	@echo "  make test        - Run tests"
	@echo "  make lint        - Run golangci-lint"
	@echo "  make vet         - Run go vet"
	@echo "  make run         - Build and run"
	@echo "  make deps        - Download and tidy dependencies"
	@echo "  make bump V=X.Y.Z - Bump VERSION, commit, and push"
	@echo "  make release     - Tag v\$$(VERSION) and push to publish a release"
	@echo "  make help        - Show this help message"
