# `/reload-cmd` — 自定义命令轻量热加载

## 背景

Reasonix 支持通过 `.md` 文件定义自定义 slash command（位于 `.reasonix/commands/`、`.claude/commands/` 等目录）。每次新增或修改命令文件后，当前必须重启应用（或 `/new`）才能生效。在多 Tab 桌面端或长时间运行的 CLI 会话中，重启中断了当前工作流。

Goal：提供一个 `/reload-cmd` 命令，不重启 Controller、不中断 MCP 连接、不重跑 Hooks，毫秒级热加载自定义命令。

> **注意：** "不修改系统提示词"指 system prompt 前缀（intro 块）不变。但 tools 缓存块会在命令集合变化时重新验证，因为 `slashCommandTool.Description()` 随命令列表变化，模型端下一轮需重新拉取 tool schemas。

## 方案选型

**选择方案 B — 轻量热加载**，理由：

- 不关 MCP、不跑 Hooks，当前会话完全无感（1-5ms 完成）
- 基于已有基础设施：`command.Load()` 是无副作用的纯函数，`Registry.Add()` 支持同名覆盖，`c.Skills()` 已有半热加载能力
- Scope 清晰：只重载命令文件，Skills 系统提示词索引由 `/skills` 或独立的 `/reload-skills` 负责

## 架构

```
/reload-cmd
    │
    ▼
Controller.ReloadCommands(ctx)
    │
    ├── 1. command.Load(dirs...) ── 重新扫盘，获取最新 .md 文件
    │
    ├── 2. 合并 Skills + Commands ── Skills 在前，Commands 在后（同名时 Command 覆盖 Skill）
    │
    ├── 3. reg.Add(NewSlashCommandTool(entries)) ── 覆盖 Registry，模型侧下轮自动生效
    │
    ├── 4. c.commands.Store(&cmds) ── atomic.Pointer 替换，人类输入 /name 即刻生效
    │
    └── CLI: 刷新 m.commands + updateCompletion() ── 补全菜单同步更新
```

## 关键流程

### 命令的消费路径

Reasonix 中有两条独立的消费路径：

| 路径 | 消费者 | 对应热加载操作 |
|------|--------|--------------|
| **模型调用** `slash_command({command, args})` | Tool Registry → `slashCommandTool.entries` | `reg.Add(NewTool)` 覆盖 |
| **人类输入** `/name args` | `Controller.CustomCommand()` → `c.commands` 遍历 | `c.commands.Store(&cmds)` |

两者都是启动时扫盘得到的快照。热加载只需将两份快照同时替换，无需重建 Controller。

### Skills 不丢失

`slash_command` tool 是一个扁平的 `map[string]SlashEntry`，Commands 和 Skills 共用命名空间。重建 tool 时必须连带当前 Skills 一起打包，否则 Skills 入口会从模型侧消失：

```go
entries := []command.SlashEntry{}
for _, sk := range c.Skills() { /* 打包 skills */ }
for _, cmd := range cmds      { /* 打包 commands */ }
c.reg.Add(command.NewSlashCommandTool(entries))
```

`c.Skills()` 在有 `skillStore` 时每次调用都扫盘，拿到的总是最新的。

## 变更文件

| 文件 | 改动说明 | 类型 |
|------|---------|------|
| `internal/command/slashtool.go` | 无需改动，使用 `NewSlashCommandTool` + `reg.Add` 覆盖即可 | — |
| `internal/control/controller.go` | `commands` 字段改为 `atomic.Pointer[[]command.Command]`；新增 `ReloadCommands(ctx)` 方法 | 核心逻辑 |
| `internal/cli/chat_tui.go` | 输入处理加 `case "/reload-cmd"`，调 `ctrl.ReloadCommands()` 并刷新 `m.commands` | CLI 前端 |
| `internal/cli/complete.go` | 补全菜单加 `/reload-cmd` 条目 | CLI 前端 |
| `internal/cli/help_view.go` | 内置命令列表加 `/reload-cmd` 条目 | CLI 前端 |
| `internal/i18n/messages_*.go` | 国际化字符串（CmdReloadCmd 等） | CLI 前端 |
| `desktop/app.go` | 加 `ReloadCommands()` 方法，桥接到 `tab.Ctrl.ReloadCommands()` | Desktop 前端 |

## 边界情况

| 场景 | 处理方式 |
|------|---------|
| **Turn 运行中** | 检查 `ctrl.Running()`，非阻塞 notice 提示用户 "wait for the current turn to finish" |
| **无命令文件变更** | 静默成功，不产生额外提示 |
| **同名命令覆盖** | `command.Load()` 保持后扫盘覆盖前扫盘的顺序，与启动行为一致 |
| **并发安全** | `reg.Add()` 内部持锁，`c.commands.Store()` 是原子操作 |
| **Desktop 无当前 Tab** | 返回错误，前端展示 |

## 测试策略

1. **单元测试**：Controller 层测试 `ReloadCommands()` — 加载一批命令 → 修改文件 → 重载 → 验证新命令可用
2. **集成测试**：验证 `Registry.Add` 同名覆盖后，agent 下一轮 `Schemas()` 返回新 tool 定义
3. **手动验证**：CLI 和 Desktop 端分别测试 `/reload-cmd` 后 `/新命令` 立刻生效