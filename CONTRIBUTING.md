# Contributing to Reasonix

Thank you for your interest in contributing to Reasonix! This guide covers
everything you need to get started.

## Prerequisites

- **Go 1.26+** — the version `go.mod` requires
- **Git** — for version control
- **Node.js 24+ and pnpm 10** (optional) — only if you work on Studio
  (`desktop/`)

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
| `internal/runtime/agent` | Agent loop, session, coordinator |
| `internal/frontend/cli` | TUI, subcommands, setup wizard |
| `internal/session/control` | Transport-agnostic controller |
| `internal/contract/config` | TOML configuration loading |
| `internal/tools/builtin` | Built-in tools (bash, read_file, …) |
| `internal/contract/provider` | Model-backend abstraction |
| `internal/model/openai` | OpenAI-compatible provider |
| `internal/ext/plugin` | MCP client (stdio + HTTP) |
| `internal/contract/event` | Typed event stream |
| `internal/ext/hook` | Shell hooks (PreToolUse, …) |
| `internal/state/memory` | REASONIX.md hierarchy + auto-memory |
| `internal/ext/skill` | Skill discovery from Markdown |
| `internal/safety/sandbox` | OS-level sandboxing |
| `internal/frontend/serve` | HTTP/SSE server frontend |
| `internal/state/checkpoint` | Snapshot-based rewind |
| `desktop/` | Studio: the Electron shell, the SPA it serves (separate Go module) |
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

### Isolated development environment

A source-built binary shares no on-disk state with a stable release when launched
with `REASONIX_HOME` set. This gives each build its own self-contained directory
tree — config, credentials, sessions, cache, skills, commands, hooks, and
desktop tab state — so the two builds never interfere:

**CLI**

```bash
REASONIX_HOME=/tmp/reasonix-dev go run ./cmd/reasonix
# or after building:
#   REASONIX_HOME=/tmp/reasonix-dev ./bin/reasonix
```

**Studio**

```bash
REASONIX_HOME=/tmp/reasonix-dev-isolated make studio
```

On Windows, use `$env:REASONIX_HOME` in PowerShell or `set REASONIX_HOME=` in
Command Prompt; the binary extension is `.exe`.

The directory is empty on first launch; the app behaves exactly like a fresh
install. Every subsequent write — config saves, credential storage, session
logs — stays under `REASONIX_HOME`. Legacy migration, OS-home convention
directory scanning, and all other fallback paths are skipped so no production
data leaks in or out.

### Cache-first review gate

Reasonix treats high prompt-cache hit rate as product behavior. Changes that
touch provider-visible system prompt construction, memory prefix, output styles,
skill index behavior, default tool surfaces, tool schemas, provider request
serialization, compaction, or MCP/tool registration need explicit cache review.

For these changes:

- Keep system prompt changes low-frequency and require explicit review.
- Say in the PR what the change does to the provider-visible prefix, and why
  that cost is worth paying.
- Prefer focused guard tests near the changed surface; `scripts/cache-guard.sh`
  remains the broader release-level cache-hit check.

### Running tests

```bash
go test ./...                           # all tests
go test ./internal/runtime/agent/ -v            # verbose, one package
go test ./internal/tools/builtin/ -run TestGrep  # one test
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

1. Create `internal/tools/builtin/mytool.go`
2. Implement the `tool.Tool` interface: `Name()`, `Description()`, `Schema()`, `ReadOnly()`, `Execute()`
3. Register via `func init() { tool.RegisterBuiltin(myTool{}) }`
4. Add tests in `internal/tools/builtin/builtin_test.go` or a separate `mytool_test.go`
5. The tool is automatically available — `main` blank-imports `builtin`

## Adding a new model provider

(For MCP tool servers see `internal/ext/plugin` instead — that's a different layer.)

1. Create `internal/contract/provider/myprovider/`
2. Implement `provider.Provider`: `Name()`, `Stream()`
3. Register via `func init() { provider.Register("mykind", New) }`
4. The provider is available from config with `kind = "mykind"`

## Adding i18n strings

1. Add the field to `internal/base/i18n/i18n.go` (`Messages` struct)
2. Add the value in `internal/base/i18n/messages_en.go` and `messages_zh.go`
3. The `TestCatalogsComplete` test will fail if you miss a locale

## Submitting changes

Reasonix ships on two lines (see the
[version roadmap](https://github.com/esengine/DeepSeek-Reasonix/discussions/10748)),
and each takes different changes:

| Line | Branch | Takes |
| --- | --- | --- |
| 2.x | `studio` | Features and fixes: active development |
| 1.x | `main-v2` | Bug fixes, provider/API compatibility, release/updater, security, platform stability |

A fix that matters on both lines lands on `main-v2` and is reviewed for
porting to `studio`; do not open the same PR against both.

1. Fork the repository
2. Create a feature branch from the branch your change targets
3. Make your changes with tests
4. Ensure `go test ./...` passes
5. Ensure `gofmt -l .` shows no changes
6. Submit a pull request to that same branch

## Reporting issues

Open an issue on GitHub with:
- Which line (1.x or 2.x) and the exact version
- Steps to reproduce
- Expected vs actual behavior
- Go version and OS
- Relevant logs or error messages

## License

By contributing, you agree that your contributions will be licensed under the
same license as the project.
