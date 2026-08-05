# meta_tool 配置项 — run_mcp 元工具

把数十个 `mcp__<server>__<tool>` 顶层工具坍缩成单个 `run_mcp` 派发器，直接缩小模型每轮看到的工具数组，缓解注意力稀释。

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
| `meta_tool` | `[tools]` 段 | `*bool` | `nil`（= false） | `true` 启用 run_mcp 派发器；`false` 或不写 = 传统 per-tool 模式 |

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
tool surface [run_mcp meta-tool]: 20 tools (0 mcp__ top-level, run_mcp registered=true)
```

- `run_mcp meta-tool` = 已启用
- `0 mcp__ top-level` = 所有 mcp__ 工具已坍缩
- `run_mcp registered=true` = 派发器已注册

未启用时显示：
```
tool surface [per-tool mcp__ (default)]: 114 tools (95 mcp__ top-level, run_mcp registered=false)
```

### 方法 2：mcp-surface-dump 对比工具

```bash
cd D:\开发\reasonix-src
go run ./cmd/mcp-surface-dump/
```

输出对比表，直接展示两种模式的工具数量和字节数差异。

## 实测数据

19 个 MCP 服务器（15 demo + 4 真实环境注入），每服务器 5 个工具：

| 模式 | 启动注册表 | mcp__ 顶层 | run_mcp | 工具数组字节 |
|------|-----------|-----------|---------|------------|
| 基线（per-tool mcp__） | 114 | 95 | 否 | 56527 |
| run_mcp 元工具 | **20** | 0 | 是 | 45257 |

- **启动注册表**：`reg.Names()` 的容量，即进入稳定缓存前缀的工具面——模型每轮都看到的 tools 数组。114 → 20，**降幅 82.5%**。
- **完整工具面**（含 boot 后追加的 skill/session 工具）：138 → 44，降幅 68.1%。
- **字节数**：56527 → 45257，降幅 19.9%。字节降幅小于条数降幅，因为 `run_mcp` 的描述里包含全部服务器→工具的映射表，但这是一条工具替代了 95 条独立 schema，对模型注意力的改善远大于字节数暗示的比例。

### 20 vs 44 的区别

| 数字 | 来源 | 含义 |
|------|------|------|
| 20 | boot.go 诊断 Notice（`reg.Names()`） | 启动时注册表容量，进入稳定缓存前缀 |
| 44 | `ToolContractEntries()` | 完整 provider 可见面，含 boot 后按 turn/session 注入的 skill 工具等 |

日常说的"工具数组容量降到 20"指的是启动注册表——这是模型每轮稳定看到的 tools 数组前缀，是注意力优化的核心指标。

## 工作原理

### 传统模式（默认）

每个 MCP 工具注册为顶层条目：
```
mcp__aether-bridge__search
mcp__aether-bridge__fetch
mcp__codebase-memory-mcp__query
...（95 条）
```
模型在 tools 数组里看到 95 条独立 schema，每条都有自己的参数定义。

### meta_tool 模式

全部坍缩成一条 `run_mcp`：
```
run_mcp  (description 里动态列出 server_name → tool_name 映射)
```

模型调用时两步：
1. 从 `run_mcp` 的 description 里查到目标服务器和工具名
2. 调用 `run_mcp`，传入 `server_name` + `tool_name` + `args`

description 示例：
```
Call an MCP tool by server_name and tool_name. Pass server_name and tool_name using the EXACT strings below.
server_name -> available tool_name values (server_name is the quoted key; tool_name is one of the comma-separated values after the colon):
  "aether-bridge": search, fetch, query, status, health
  "codebase-memory-mcp": query, store, recall, list, delete
  ...
Call with {"server_name": <one of the quoted keys above>, "tool_name": <one of that server's listed tools>, "args": <object>}. Do not swap server_name and tool_name.
```

description 每次调用时从已连接的 Host 客户端 + 磁盘缓存 schema 动态拼接，按服务器名和工具名排序保证字节稳定（缓存前缀不抖动）。

## 注意事项

- **默认关闭**：不写 `meta_tool = true` 时行为与之前完全一致，不影响现有用户。
- **服务器热加**：`/mcp add` 添加的服务器会自动出现在 `run_mcp` 的 description 映射里（通过 `Host.ServerNames()`），无需重启。
- **首回合正确性**：后台 spawn 未完成时，`run_mcp` 从磁盘缓存 schema 读取工具列表，与传统的 cache-hit 占位行为一致。
- **图片内容**：`run_mcp` 实现了 `tool.ImageTool` 接口，MCP 返回的图片内容能正确传递给视觉模型，不会被扁平化成文本占位符。
- **Plan 模式**：`run_mcp` 标记为非只读（`ReadOnly() = false`），因为它可能派发到写入工具。Plan 模式下仍受 approval gate 约束。

## 相关文件

| 文件 | 作用 |
|------|------|
| `internal/plugin/metatool.go` | `run_mcp` 派发器核心实现（Description 动态拼接 + Execute 派发） |
| `internal/config/config.go` | `ToolsConfig.MetaTool` 字段 + `MCPMetaToolEnabled()` 解析方法 |
| `internal/boot/boot.go` | 启动时根据 `cfg.MCPMetaToolEnabled()` 决定注册模式 |
| `internal/plugin/meta_capacity_test.go` | 容量数学验证（纯逻辑，不依赖 boot） |
| `internal/plugin/metatool_test.go` | 描述契约 / schema / 参数校验测试 |
| `internal/config/meta_tool_test.go` | config/env 优先级矩阵测试（24 项） |
| `cmd/mcp-surface-dump/main.go` | 实机对比工具，证明配置文件路径端到端有效 |
