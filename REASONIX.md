# Reasonix project memory

This file is loaded into every session's system prompt (the cache-stable prefix),
so keep it concise and durable — it is the project's standing instructions to the
agent. It is the Reasonix analog of Claude Code's CLAUDE.md.

## Conventions

- Go kernel under `internal/`; each package owns one concern and documents it in a
  package comment. Match the surrounding comment density and idiom when editing.
- One transport-agnostic `control.Controller` sits behind every frontend (chat
  TUI, HTTP/SSE serve, Wails desktop). Add behavior to the controller, not a
  frontend, so all three inherit it.
- Cache-first: the system-prompt prefix (base prompt + tools + memory) must stay
  byte-stable across turns so DeepSeek's automatic prefix cache stays warm. Never
  mutate it mid-session — ride the turn tail instead (see `control.Compose`).

## Memory

- Hierarchical docs: `REASONIX.md` (this file, committed/shared), `REASONIX.local.md`
  (personal, git-ignored), user-global `~/.config/reasonix/REASONIX.md`, and any
  `REASONIX.md` in an ancestor dir. `AGENTS.md` is accepted as a fallback name.
- `@path` on its own line imports another file's contents.
- `#<note>` in chat quick-adds a line here. The `remember` tool saves durable
  facts to the per-project auto-memory store (frontmatter files + `MEMORY.md`
  index), which loads into the prefix on the next session.

## Notes

<<<<<<< HEAD
## 禁止操作

| # | 禁止 | 原因 | 正确做法 |
|---|------|------|----------|
| 1 | 禁止在 planner 里写实现 | planner 只能只读调研，写了 executor 也不执行 | planner 输出计划文本即可 |
| 2 | 禁止手动改 `internal/config/config.go` 的默认值 | 默认值通过 `render.go` 的注释模板输出 | 改 `reasonix.example.toml` 或文档 |
| 3 | 禁止全量读取大文件（>300 行） | `controller.go` 3700+ 行，全量读浪费上下文 | 先 `grep` 关键词再读匹配段落 |
| 4 | 禁止在非 `cli/` `main/` 包中调 `os.Exit` | 库代码不应决定进程生命周期 | 返回 error 让上层处理 |
| 5 | 禁止修改 `docs/` 下文件时不更新文件地图 | 文档结构变更后，REASONIX.md 会指向不存在的位置 | 同步更新本文档的目录用途 |

## 致命陷阱

| # | 陷阱 | 表现 | 规则 |
|---|------|------|------|
| 1 | prefix cache 断裂 | 每轮 token 消耗翻倍 | 不修改 system prompt / tool schema / memory 前缀顺序；改动通过 `Compose` 骑在 turn tail 上 |
| 2 | Windows 路径反斜杠 | bash 工具在 Windows 上收到 `\` 被转义 | 给 bash 的路径统一用正斜杠 `/`，Python 脚本用 `Path()` |
| 3 | Goal 模式空的 todo 列表 | agent 喊 `[goal:complete]` 但没调过 `todo_write` | 先 `todo_write` 建列表再执行，不跳过 |
| 4 | 死锁：worker 等 spawnCh 不关闭 | `parallel_tasks` 卡死 | dispatcher 结束时先 `close(spawnCh)` 再 `close(allDone)` |
| 5 | 子 agent 递归调用 `parallel_tasks` | 无限嵌套 | 确认 `parallel_tasks` 在 `subagentMetaTools` 排除列表中 |
=======
## Pre-push CI simulation

Run these **before every commit** to catch the fastest CI failures locally:

```bash
gofmt -w .                          # catches gofmt (saves ~13s CI)
go vet ./...                        # catches vet warnings (saves ~52s CI/lint)
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

## Cache-impact PR metadata

When PR changes touch files under `internal/boot/`, `internal/tool/`, `internal/provider/`, or other cache-sensitive paths (listed in `scripts/check-cache-impact.sh`), the PR body MUST include these lines at the end:

```
Cache-impact: <none|low|medium|high> — <reason>
Cache-guard: <focused guard test/command or existing guard rationale>
```

If the PR also touches files under `internal/config/`, `internal/memory/`, `internal/outputstyle/`, `internal/skill/`, or `internal/boot/`, add:

```
System-prompt-review: <reviewer/approval note>
```

Values `n/a`, `none`, `todo`, `tbd` are rejected — use a descriptive reason instead.
>>>>>>> upstream/main-v2
