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

## Git 提交规范

遵循 [Conventional Commits](https://www.conventionalcommits.org/) 规范，同时满足以下要求：

### 提交格式

```
<type>(<scope>): <subject>

<详细描述背景、修改内容和核心逻辑>

修改内容：
- <文件/模块> <具体改动>

特性/影响：
- <业务逻辑说明或影响范围>

Co-Authored-By: Reasonix <noreply@reasonix.com>
```

### 规则

- **type** 限定：`feat`、`fix`、`docs`、`style`、`refactor`、`test`、`chore`、`perf`、`ci`、`build`、`revert`
- **scope** 使用英文模块名（如 `agent`、`desktop`、`tool`、`control`）或工单号
- **subject** 不超过 50 字，清晰描述改动
- **强制多行提交**：必须包含详细描述、修改内容清单、影响范围
- **强制 Co-Authored-By**：末尾必须追加 `Co-Authored-By: Reasonix <noreply@reasonix.com>`
- 提交信息必须结合实际改动生成，禁止空泛模板

### 示例

```bash
feat(desktop): add MCP drawer skill metadata badges

详细描述：
新增 MCP 抽屉中的技能元数据徽章展示，包括版本、作者信息

修改内容：
- internal/desktop/mcp_drawer.go 新增详情面板布局
- internal/desktop/skill_badge.go 实现元数据徽章组件

特性/影响：
- 用户可查看 MCP 工具的详细元数据
- 技能展示增加版本和作者信息

Co-Authored-By: Reasonix <noreply@reasonix.com>
```

## Notes
