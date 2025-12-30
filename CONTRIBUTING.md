# Contributing to GoplexCLI

Thank you for your interest in contributing to GoplexCLI! This document provides guidelines and instructions for contributing.

## Code of Conduct

Be respectful, inclusive, and collaborative. We're all here to make a better tool.

## Getting Started

1. Fork the repository
2. Clone your fork: `git clone https://github.com/YOUR_USERNAME/goplexcli.git`
3. Create a feature branch: `git checkout -b feature/amazing-feature`
4. Make your changes
5. Test your changes
6. Commit with a descriptive message
7. Push to your fork: `git push origin feature/amazing-feature`
8. Open a Pull Request

## Development Setup

### Prerequisites

- Go 1.20 or higher
- Make (optional, but recommended)
- fzf, mpv, and rclone for testing

### Building

```bash
make build
```

Or without make:

```bash
go build -o goplexcli ./cmd/goplexcli
```

### Running Tests

```bash
make test
```

Or:

```bash
go test ./...
```

## Project Structure

```
goplexcli/
├── cmd/goplexcli/     # Main CLI application entry point
├── internal/          # Internal packages
│   ├── cache/        # Media caching logic
│   ├── config/       # Configuration management
│   ├── download/     # Rclone download integration
│   ├── player/       # MPV player integration
│   ├── plex/         # Plex API client wrapper
│   └── ui/           # fzf and UI components
└── ...
```

## Coding Guidelines

### Go Style

- Follow standard Go formatting (`gofmt`)
- Use meaningful variable and function names
- Add comments for exported functions and types
- Keep functions focused and concise
- Handle errors explicitly

### Commit Messages

Use clear, descriptive commit messages:

```
Add feature for sorting media by rating

- Implement sorting in cache module
- Add --sort flag to browse command
- Update documentation
```

### Pull Request Guidelines

- Keep PRs focused on a single feature or fix
- Update documentation as needed
- Add tests for new functionality
- Ensure all tests pass
- Reference any related issues

## Areas for Contribution

### Features

- Additional playback options
- Support for music libraries
- Playlist management
- Watch history tracking
- Resume playback support
- Multiple server support

### Improvements

- Better error handling and messages
- Performance optimizations
- Enhanced caching strategies
- Improved UI/UX
- More configuration options

### Documentation

- More detailed setup guides
- Usage examples
- Video tutorials
- Translation to other languages

### Bug Fixes

Check the [Issues](https://github.com/joshkerr/goplexcli/issues) page for bugs to fix.

## Testing

When adding new features, please include tests:

```go
func TestFeature(t *testing.T) {
    // Test implementation
}
```

## Dependencies

When adding new dependencies:

1. Use well-maintained, popular libraries when possible
2. Keep dependencies minimal
3. Run `go mod tidy` after adding dependencies
4. Update documentation if the dependency requires user setup

## Questions?

Feel free to:

- Open an issue for discussion
- Ask questions in pull requests
- Reach out to maintainers

Thank you for contributing! 🎉
