# Contributing to Reasonix

Thank you for your interest in contributing to Reasonix! This guide covers
everything you need to get started.

## Prerequisites

- **Go 1.25+** — the project targets the latest stable Go release
- **Git** — for version control
- **Node.js** (optional) — only if you work on the desktop app (`desktop/`)

## Getting started

```bash
git clone https://github.com/esengine/DeepSeek-Reasonix.git
cd DeepSeek-Reasonix
go build ./cmd/reasonix    # builds the CLI binary
go test ./...              # runs the full test suite
```

## Project structure

| Directory | Purpose |
|-----------|---------|
| `cmd/reasonix` | CLI entry point |
| `internal/agent` | Agent loop, session, coordinator |
| `internal/cli` | TUI, subcommands, setup wizard |
| `internal/control` | Transport-agnostic controller |
| `internal/config` | TOML configuration loading |
| `internal/tool/builtin` | Built-in tools (bash, read_file, …) |
| `internal/provider` | Model-backend abstraction |
| `internal/provider/openai` | OpenAI-compatible provider |
| `internal/plugin` | MCP client (stdio + HTTP) |
| `internal/event` | Typed event stream |
| `internal/hook` | Shell hooks (PreToolUse, …) |
| `internal/memory` | REASONIX.md hierarchy + auto-memory |
| `internal/skill` | Skill discovery from Markdown |
| `internal/sandbox` | OS-level sandboxing |
| `internal/serve` | HTTP/SSE server frontend |
| `internal/checkpoint` | Snapshot-based rewind |
| `desktop/` | Wails-based desktop app (separate Go module) |
| `docs/` | Engineering spec, migration guide |

### Dependency direction

```
cli → {agent, plugin, config} → {tool, provider}
```

Built-in subpackages import their parent to self-register via `init()`.
Parents never import children.

## Development workflow

### Building

```bash
make build          # go build ./...
make test           # go test ./...
make vet            # go vet ./...
make fmt            # gofmt -w .
make hooks          # install git hooks (pre-push: go vet)
make cross          # cross-compile for all 6 targets
```

### Running tests

```bash
go test ./...                           # all tests
go test ./internal/agent/ -v            # verbose, one package
go test ./internal/tool/builtin/ -run TestGrep  # one test
```

### Code style

- `gofmt` is enforced by CI — format before committing
- Follow existing patterns: wrap errors with `fmt.Errorf("...: %w", err)`
- Library code never calls `os.Exit` or prints to stdout/stderr
- Only `cli/` and `main/` decide exit codes and user-facing messages
- Exported identifiers must have doc comments

### Commit messages

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```
feat(glob): add ** recursive pattern support
fix: replace silent error discards with structured logging
test(event): add comprehensive unit tests for event package
docs: add CONTRIBUTING.md
ci: add golangci-lint and govulncheck
```

## Adding a new built-in tool

1. Create `internal/tool/builtin/mytool.go`
2. Implement the `tool.Tool` interface: `Name()`, `Description()`, `Schema()`, `ReadOnly()`, `Execute()`
3. Register via `func init() { tool.RegisterBuiltin(myTool{}) }`
4. Add tests in `internal/tool/builtin/builtin_test.go` or a separate `mytool_test.go`
5. The tool is automatically available — `main` blank-imports `builtin`

## Adding a new model provider

(For MCP tool servers see `internal/plugin` instead — that's a different layer.)

1. Create `internal/provider/myprovider/`
2. Implement `provider.Provider`: `Name()`, `Stream()`
3. Register via `func init() { provider.Register("mykind", New) }`
4. The provider is available from config with `kind = "mykind"`

## Adding i18n strings

1. Add the field to `internal/i18n/i18n.go` (`Messages` struct)
2. Add the value in `internal/i18n/messages_en.go` and `messages_zh.go`
3. The `TestCatalogsComplete` test will fail if you miss a locale

## Submitting changes

1. Fork the repository
2. Create a feature branch from `main-v2`
3. Make your changes with tests
4. Ensure `go test ./...` passes
5. Ensure `gofmt -l .` shows no changes
6. Submit a pull request to `main-v2`

## Reporting issues

Open an issue on GitHub with:
- Steps to reproduce
- Expected vs actual behavior
- Go version and OS
- Relevant logs or error messages

## License

By contributing, you agree that your contributions will be licensed under the
same license as the project.
