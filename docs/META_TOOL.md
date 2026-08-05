# meta_tool 配置项 — use_capability 代理

把数十个 `mcp__<server>__<tool>` 顶层工具坍缩成单个 `use_capability` 代理，直接缩小模型每轮看到的工具数组，缓解注意力稀释。

## 快速启用

在项目的 `reasonix.toml` 中加两行：

```toml
[tools]
meta_tool = true
```

重启即可。默认 `false`（不写该行 = 关闭），完全保持原有行为。

## 配置项

| 字段 | 位置 | 类型 | 默认 | 说明 |
|------|------|------|------|------|
| `meta_tool` | `[tools]` 段 | `*bool` | `nil`（= false） | `true` 启用 use_capability 代理；`false` 或不写 = 传统 per-tool 模式 |

`*bool` 指针类型区分"未设置"和"显式 false"——不写该行与写 `meta_tool = false` 效果相同（都关闭），但语义上"未设置"表示用户没表态，保留未来默认值变更的余地。

## 优先级

三层解析，高优先级覆盖低优先级：

```
1. 环境变量 REASONIX_MCP_META_TOOL   ← 最高，用于 CI / 临时测试 / mcp-surface-dump
2. 配置项 [tools] meta_tool           ← 持久化，per-project
3. 默认 false                         ← 不写任何东西时的行为
```

环境变量识别的拼写（大小写不敏感，自动 trim 空格）：

| 值 | 含义 |
|----|------|
| `1` `true` `yes` `on` | 启用 |
| `0` `false` `no` `off` | 禁用 |
| 未设置 / 空字符串 / 其他 | 不覆盖，回退到配置项 |

### 使用场景

**持久启用**（推荐生产环境）：写配置文件，所有会话自动生效。
```toml
[tools]
meta_tool = true
```

**临时启用**（不改配置文件，单次运行）：
```bash
# Linux/macOS
REASONIX_MCP_META_TOOL=1 reasonix

# Windows PowerShell
$env:REASONIX_MCP_META_TOOL = "1"; reasonix
```

**临时禁用**（配置里写了 true，但某次运行想关掉）：
```bash
REASONIX_MCP_META_TOOL=0 reasonix
```

## 验证是否生效

### 方法 1：启动诊断 Notice

设置 `REASONIX_DUMP_TOOL_SURFACE=1` 启动，boot 会输出一条 Notice：

```bash
REASONIX_DUMP_TOOL_SURFACE=1 reasonix
```

输出示例：
```
tool surface [use_capability meta-tool]: 48 tools (0 mcp__ top-level, use_capability registered=true)
```

- `use_capability meta-tool` = 已启用
- `0 mcp__ top-level` = 所有 mcp__ 工具已隐藏
- `use_capability registered=true` = 代理已注册

未启用时显示：
```
tool surface [per-tool mcp__ (default)]: 142 tools (95 mcp__ top-level, use_capability registered=false)
```

### 方法 2：mcp-surface-dump 对比工具

```bash
cd D:\开发\reasonix-src
go run ./cmd/mcp-surface-dump/
```

输出对比表，直接展示两种模式的工具数量和字节数差异。

## 实测数据

由 `cmd/mcp-surface-dump` 走真实 `boot.Build` 路径测得（47 个内置工具 + 15 demo MCP 服务器 × 5 工具 + 4 个已安装包/legacy 服务器）：

| 模式 | 注册表容量 | mcp__ 顶层 | use_capability | 工具数组字节 |
|------|-----------|-----------|----------------|------------|
| 基线（per-tool mcp__） | 142 | 95 | 否 | 65010 |
| use_capability 代理 | **48** | 0 | 是 | 52826 |

- **注册表容量**：`reg.Names()` 的容量——模型每轮都看到的 tools 数组。142 → 48，**降幅 66.2%**。95 个 mcp__ 顶层条目全部隐藏，仅保留 47 个内置工具 + 1 个 use_capability。
- **字节数**：65010 → 52826，**降幅 18.7%**。字节降幅低于条数降幅，因为内置工具的 schema 体积占比更大；但 MCP schema 条目（每条含独立参数定义）的注意力稀释被完全消除。
- **关键收益**：模型不再需要在 95 条 mcp__ schema 间分配注意力，全部 MCP 调用走 `use_capability` 的固定三动作模型（list/inspect/call）。

## 工作原理

### 传统模式（默认）

每个 MCP 工具注册为顶层条目：
```
mcp__github__search_issues
mcp__github__create_issue
mcp__filesystem__read_file
...（95 条）
```
模型在 tools 数组里看到 95 条独立 schema，每条都有自己的参数定义。

### meta_tool 模式

全部坍缩成一条 `use_capability`：
```
use_capability  (固定 schema: list / inspect / call / decline)
```

模型调用时通过三动作模型：
1. **list** — 列出已配置的 MCP 服务器（不启动进程）
2. **inspect** — 查看特定工具的 schema 和元数据
3. **call** — 调用 MCP 工具（按需连接，走完整安全流程）

调用示例：
```json
{"action": "list"}
{"action": "inspect", "capability_id": "mcp-server:github"}
{"action": "call", "capability_id": "mcp-tool:github/search_issues", "arguments": {"query": "..."}}
```

### 安全保障

use_capability 通过 `CallResolver` 接口实现完整安全流程：

1. **ResolveCall** 解析 `capability_id` 到真实工具名 `mcp__<server>__<tool>`
2. Agent 对真实工具名执行 **permission gate**（allow/ask/deny）
3. **PreToolUse hooks** 对真实工具名和参数执行
4. **Target.Execute** 走 `remoteTool.ExecuteWithImages`（含 server authorization、destructive/readOnly 复核）
5. **PostToolUse hooks** 和 **evidence/mutation tracking** 对真实工具名记录

关键区别：
- **spec 身份验证**：通过 `MCPRuntimeSpecMatches` 确保不会跨 controller/项目调用同名但不同身份的 server
- **懒启动**：未连接 server 返回延迟 Target，仅在 Execute 时按需连接（不 KickSpawns）
- **generation 安全**：remove 后旧 generation 不会通过后台握手复活
- **固定 schema**：`Schema()` 和 `Description()` 是常量，不随连接/热加/工具漂移变化

## 注意事项

- **默认关闭**：不写 `meta_tool = true` 时行为与之前完全一致，不影响现有用户。
- **首回合延迟**：旧模式在 boot 时立即启动全部握手；新模式首次 `call` 触发按需连接（慢一次）。这是懒启动的权衡——避免启动不需要的 MCP 进程。
- **auto_start=false 服务器**：`list` 可见但 `call` 返回 "disabled"。
- **图片内容**：`onDemandMCPTool` 实现了 `tool.ImageTool` 接口，MCP 返回的图片内容能正确传递给视觉模型。
- **Plan 模式**：`use_capability.ReadOnly() = true`，但 `resolveCall` 返回的 `TargetName` 是真实工具名，plan-mode 会对真实目标名做 `planModeDecision`。

## 相关文件

| 文件 | 作用 |
|------|------|
| `internal/agent/usecapability.go` | `UseCapabilityTool` 实现（ResolveCall + list/inspect/call + 安全流程） |
| `internal/config/config.go` | `ToolsConfig.MetaTool` 字段 + `MCPMetaToolEnabled()` 解析方法 |
| `internal/boot/boot.go` | 启动时根据 `cfg.MCPMetaToolEnabled()` 注册 use_capability |
| `internal/plugin/metatool.go` | 仅保留 `MetaToolEnabled()` env shim（历史） |
| `internal/plugin/meta_capacity_test.go` | 容量数学验证（纯逻辑，不依赖 boot） |
| `internal/agent/usecapability_metatool_test.go` | 回归测试：身份隔离/deny匹配/懒启动/schema稳定 |
| `internal/config/meta_tool_test.go` | config/env 优先级矩阵测试 |
| `cmd/mcp-surface-dump/main.go` | 实机对比工具，证明配置文件路径端到端有效 |
