# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## About This Project

Echo is a high performance, minimalist Go web framework. This is the main repository for Echo v4, which is available as a Go module at `github.com/labstack/echo/v4`.

## Development Commands

The project uses a Makefile for common development tasks:

- `make check` - Run linting, vetting, and race condition tests (default target)
- `make init` - Install required linting tools (golint, staticcheck)
- `make lint` - Run staticcheck and golint
- `make vet` - Run go vet
- `make test` - Run short tests
- `make race` - Run tests with race detector
- `make benchmark` - Run benchmarks

Example commands for development:
```bash
# Setup development environment
make init

# Run all checks (lint, vet, race)
make check

# Run specific tests
go test ./middleware/...
go test -race ./...

# Run benchmarks
make benchmark
```

## Code Architecture

### Core Components

**Echo Instance (`echo.go`)**
- The `Echo` struct is the top-level framework instance
- Contains router, middleware stacks, and server configuration
- Not goroutine-safe for mutations after server start

**Context (`context.go`)**
- The `Context` interface represents HTTP request/response context
- Provides methods for request/response handling, path parameters, data binding
- Core abstraction for request processing

**Router (`router.go`)**
- Radix tree-based HTTP router with smart route prioritization
- Supports static routes, parameterized routes (`/users/:id`), and wildcard routes (`/static/*`)
- Each HTTP method has its own routing tree

**Middleware (`middleware/`)**
- Extensive middleware system with 50+ built-in middlewares
- Middleware can be applied at Echo, Group, or individual route level
- Common middleware: Logger, Recover, CORS, JWT, Rate Limiting, etc.

### Key Patterns

**Middleware Chain**
- Pre-middleware runs before routing
- Regular middleware runs after routing but before handlers
- Middleware functions have signature `func(next echo.HandlerFunc) echo.HandlerFunc`

**Route Groups**
- Routes can be grouped with common prefixes and middleware
- Groups support nested sub-groups
- Defined in `group.go`

**Data Binding**
- Automatic binding of request data (JSON, XML, form) to Go structs
- Implemented in `binder.go` with support for custom binders

**Error Handling**
- Centralized error handling via `HTTPErrorHandler`
- Automatic panic recovery with stack traces

## File Organization

- Root directory: Core Echo functionality (echo.go, context.go, router.go, etc.)
- `middleware/`: All built-in middleware implementations
- `_test/`: Test fixtures and utilities
- `_fixture/`: Test data files

## Code Style

- Go code uses tabs for indentation (per .editorconfig)
- Follows standard Go conventions and formatting
- Uses gofmt, golint, and staticcheck for code quality

## Testing

- Standard Go testing with `testing` package
- Tests include unit tests, integration tests, and benchmarks
- Race condition testing is required (`make race`)
- Test files follow `*_test.go` naming convention