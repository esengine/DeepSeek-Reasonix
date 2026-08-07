# Reasonix project memory

This file is loaded into every session's system prompt (the cache-stable prefix),
so keep it concise and durable — it is the project's standing instructions to the
agent. It is the Reasonix analog of Claude Code's CLAUDE.md.

## Conventions

- Go kernel under `internal/`; each package owns one concern. A package's long
  explanation belongs in its `doc.go`, not spread across implementation files.
- One transport-agnostic `control.Controller` sits behind every frontend (chat
  TUI, HTTP/SSE serve, Wails desktop). Add behavior to the controller, not a
  frontend, so all three inherit it.
- Layering (enforced): utility packages import nothing under `reasonix/`; only
  the frontends `cli`, `serve`, `acp`, `bot`, `botruntime`, `boot` and the hosts
  `cmd/`, `desktop/` may import `control`; nothing below a frontend may import
  one. The declared sets live in `tools/repolint/layers.go`.
- Cache-first: the system-prompt prefix (base prompt + tools + memory) must stay
  byte-stable across turns so DeepSeek's automatic prefix cache stays warm. Never
  mutate it mid-session — ride the turn tail instead (see `control.Compose`).

## Comments

Default is none — the code is the truth. Write one only when the **why** is
non-obvious: a hidden constraint, a workaround anchored to something verifiable,
an invariant the type system cannot express, or an external-protocol quirk.

- Declaration doc: ≤5 lines. Package comment: ≤8 lines, or ≤40 in a `doc.go`.
- Every other comment: ≤3 lines. Struct-field and trailing `//`: 1 line.
- Never: restatements of the code, phase/stage narrative, incident or
  conversation history, section banners, commented-out code, `@param` lists.
- `TODO(#nnn):` and `HACK(#nnn):` need the issue anchor. `FIXME` is banned.
- One responsibility per file; 800 lines is the ceiling.

`go run ./tools/repolint` enforces all of it against a ratchet baseline: recorded
debt is tolerated, anything new fails CI. Never widen the baseline to land a
change — fix the code. `-update` exists for carrying debt through a rename or an
extraction, and that diff must be justified in the PR.

## Memory

- Standing instructions are hierarchical: committed/shared `REASONIX.md`,
  `AGENTS.md`, and `CLAUDE.md`; personal `*.local.md` variants; matching files in
  ancestor directories; and user-global files under the memory state root
  (`REASONIX_STATE_HOME`, otherwise `REASONIX_HOME`, otherwise `~/.reasonix` on
  macOS/Linux or `%APPDATA%\reasonix` on Windows). All distinct supported files
  in a directory load; `AGENTS.md` is not merely a fallback.
- `@path` on its own line imports another file's contents.
- `#<note>` in chat quick-adds an always-on instruction. The `remember` tool
  instead saves a fallible background fact (frontmatter file + `MEMORY.md`
  index). Fact `type` classifies content; independent `scope` controls whether it
  is project-only (the default) or explicitly global. The index loads into the
  stable prefix on the next session; global user/feedback bodies also load as
  lower-priority compatibility guidance. The current turn receives a tail note.

## Notes

## Pre-push CI simulation

Run these **before every commit** to catch the fastest CI failures locally:

```bash
gofmt -w .                          # catches gofmt (saves ~13s CI)
go vet ./...                        # catches vet warnings (saves ~52s CI/lint)
go run ./tools/repolint             # catches comment/size/layering regressions
go test ./internal/tool/builtin/ ./internal/boot/  # catches tool/boot test breaks
```

CI runs `golangci-lint` (not locally available), but gofmt + vet already block ~80% of fast-fail scenarios.

## Import cycle rule

Before importing a new internal package from a non-test file, verify the target package's **test files** aren't already importing back to you:

```
# BAD: agent(_test.go) → tool/builtin(sessions.go) → agent  → setup failed
```

Use `go test ./path/to/target/` to detect cycles **before** pushing. A `[setup failed]` message means a cycle exists.

## PR hygiene

- **One force-push per round of review feedback.** Multiple force-pushes destroy review history and confuse reviewers.
- **Keep the PR diff minimal.** Only the files relevant to the PR's purpose — no stray changes from other branches.
- **Amend, don't add commits, for review feedback** — keeps the commit history clean.

## PR metadata gates

Two CI guards read the PR body. The scripts are the source of truth and both
run locally: `scripts/check-cache-impact.sh`, `scripts/check-docs-impact.sh`.
Separators must be an ASCII `-` or `:` — an em dash fails the docs guard.

Cache-sensitive diffs (`internal/tool/`, `internal/provider/`,
`internal/boot/`, `internal/agent/agent.go`, and the rest of the list in the
script) require:

```
Cache-impact: <none|low|medium|high> - <reason>
Cache-guard: <focused guard test/command or existing guard rationale>
```

`none` is a legitimate impact when the provider-visible prefix stays
byte-identical; only an empty value, `todo`, or `tbd` is rejected. If the diff
also touches `internal/config/`, `internal/memory/`, `internal/outputstyle/`,
`internal/skill/`, or `internal/boot/`, add `System-prompt-review: <note>` —
that field additionally rejects `none` and `n/a`, so it must name a reviewer.

User-visible diffs (`cmd/reasonix/`, `desktop/`, `npm/`, and most of
`internal/`; tests and lockfiles are exempt) require one of these, chosen by
whether the same PR edited `docs/*.md`:

```
Documentation-impact: updated - <what changed>            # docs/*.md edited
Documentation-impact: none - <why the docs stay correct>  # not edited
```
