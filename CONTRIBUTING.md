# Contributing to wally-tunnel

Thanks for your interest in contributing! This guide will help you get started.

## Development Setup

**Prerequisites:** Go 1.23+

```bash
git clone https://github.com/wgawan/wally-tunnel.git
cd wally-tunnel
make build
```

## Running Tests

```bash
# All tests with race detection
go test -race -count=1 ./...

# With coverage
go test -race -cover ./...

# Specific package
go test -race ./internal/server/...
```

## Code Style

- Run `gofmt` and `goimports` before committing
- Run `go vet ./...` to catch common issues
- Follow standard Go conventions (Effective Go, Go Code Review Comments)

## Making Changes

1. Fork the repository
2. Create a feature branch: `git checkout -b my-feature`
3. Make your changes
4. Ensure tests pass: `go test -race ./...`
5. Ensure vet passes: `go vet ./...`
6. Commit with a descriptive message (see below)
7. Push and open a pull request

## Commit Messages

Use conventional commit format:

```
type: short description

Optional longer explanation.
```

Types: `feat`, `fix`, `refactor`, `docs`, `test`, `chore`, `perf`

## Pull Requests

- Keep PRs focused on a single change
- Include tests for new functionality
- Update documentation if behavior changes
- Ensure CI passes before requesting review

## Reporting Bugs

Open an issue with:
- What you expected to happen
- What actually happened
- Steps to reproduce
- Go version, OS, and wally-tunnel version

## Feature Requests

Open an issue describing:
- The problem you're trying to solve
- Your proposed solution (if any)
- Any alternatives you've considered

## License

By contributing, you agree that your contributions will be licensed under the [MIT License](LICENSE).
