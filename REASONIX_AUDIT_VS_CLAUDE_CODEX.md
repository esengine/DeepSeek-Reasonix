# Reasonix 对标 Claude Code / Codex CLI 完整审计报告

> 生成日期: 2026-08-05
> 审计范围: Agent 核心 + 基础设施 + Skill 系统 + MCP 系统
> 审计方法: 静态代码审查 + 生产调用链验证 + 三路并行审计
> 对标对象: Claude Code (claude.ai/cli) + Codex CLI (OpenAI)

## 〇、审计方法与证据标准

本报告每一条问题均通过以下方式交叉验证：
1. **生产调用链搜索**：在非测试、非定义代码中搜索方法/字段引用，0 命中即判定为死代码
2. **文件级证据**：所有结论标注 `文件:行号`，可点击复核
3. **实测编译验证**：关键文件已 `go build` / `go vet` 通过

三路审计分工：
- **Agent 核心审计**（前序完成）：工具系统、子代理、Plan Mode、TodoWrite、Context 压缩、流式输出
- **基础设施审计**（子代理 4679e544 完成）：OPT 模块、Hooks、沙箱、会话恢复、巨型文件
- **Skill/MCP 审计**（本次完成）：Skill 加载/索引/缓存、MCP 工具注入/注意力

---

## 一、问题清单总览（按严重度）

| # | 严重度 | 子系统 | 问题 | 对标差距 |
|---|--------|--------|------|----------|
| P0-1 | 🔴 致命 | MCP | 无 run_mcp 元工具，所有 MCP 工具注册为顶层工具淹没模型上下文 | Claude Code 用单一 run_mcp 元工具按需发现 |
| P0-2 | 🔴 致命 | MCP | ContextualToolFilter (OPT-50) 死代码，零动态工具过滤 | Claude Code 无此问题（元工具天然过滤） |
| P0-3 | 🔴 致命 | Agent | 16+ OPT v2/v3 模块浅集成，核心功能方法从未接入生产路径 | 系统性纸面完成 |
| P0-4 | 🔴 致命 | Skill | SkillActivationCache (OPT-35) 幽灵实现，loadFromDisk/saveToDisk 为空 | 声称的缓存优化不存在 |
| P1-5 | 🟠 严重 | Agent | WebSearch 工具缺失 | Claude Code/Codex 均内置 |
| P1-6 | 🟠 严重 | Agent | 并行子代理仅限只读，无并行写子代理 | Claude Code 支持并行写 |
| P1-7 | 🟠 严重 | Agent | EnterPlanMode/ExitPlanMode 工具缺失，Plan Mode 模型不可控 | Claude Code 模型可主动进入 |
| P1-8 | 🟠 严重 | Agent | TodoWrite 缺 id/priority/summary/merge 字段 | Claude Code Tasks v2 更完整 |
| P1-9 | 🟠 严重 | MCP | cache-miss 两轮握手：模型须调 connect stub 后等下一轮 | Claude Code 单轮发现 |
| P2-10 | 🟡 中 | MCP | cache pinning 导致会话内 MCP 工具变更不可见 | 元工具无此问题 |
| P2-11 | 🟡 中 | Hooks | 缺 prompt 类型 Hook（仅 command 类型） | Claude Code 支持 prompt Hook |
| P2-12 | 🟡 中 | 会话 | 缺 CLI 级 --resume 标志 | Claude Code 支持 |
| P2-13 | 🟡 中 | MCP | SSE 传输 CLI 接受 --sse 但运行时报错 | Claude Code 弃用但仍支持 |
| P2-14 | 🟡 中 | Skill | 索引盲截断 16000 字符，不按相关性排序 | — |
| P2-15 | 🟡 中 | Agent | review_gate.go glob 匹配/签名生成为"简化版" | — |
| P3-16 | 🟢 低 | 结构 | agent.go 248KB God file（含 200+ OPT 字段） | — |
| P3-17 | 🟢 低 | 结构 | 多个 God file（app.go 275KB, controller.go 186KB, chat_tui.go 143KB） | — |
| P3-18 | 🟢 低 | Skill | body 预加载与"on demand"声明矛盾 | — |

---

## 二、P0 致命问题详解

### P0-1 / P0-2：MCP 工具淹没模型上下文（用户"AI 注意力"问题根因）

**现象**：用户反馈"MCP requires major modifications to address AI attention limitations"。模型在配置多 MCP 服务器时"搞不清"任务。

**根因（代码证据）**：

Reasonix 将每个 MCP 工具注册为独立的顶层工具，命名空间为 `mcp__<server>__<tool>`，全部进入 provider 请求的 tools 数组：

- [plugin.go:1182-1191](file:///D:/开发/reasonix-src/internal/plugin/plugin.go#L1182-L1191) — `listTools` 把每个 MCP 工具包装成 `remoteTool` 注册
- [plugin.go:1240-1242](file:///D:/开发/reasonix-src/internal/plugin/plugin.go#L1240-L1242) — `toolName` 生成 `mcp__<server>__<tool>` 顶层名

用户当前配置 15+ MCP 服务器（integrated_code_mode, DesktopCommander, Everything, GitHub, Memory, OpenRPC, Puppeteer, Sequential_Thinking, apple_shortcuts, context7, kubernetes, maven, mcp-k8s-go, mobile-mcp, notionApi, pdf-reader-mcp, windows-cli），每个暴露多个工具，**潜在 50-100+ MCP 工具全部作为顶层工具注入模型上下文**。叠加 19 个内置工具 + skill 索引，模型每轮面对 100+ 工具定义，注意力被严重稀释。

**对标 Claude Code**：Claude Code 使用单一 `run_mcp` 元工具，模型按需通过 `server_name + tool_name` 发现并调用 MCP 工具。tools 数组只多一个条目，不随 MCP 服务器数量膨胀。这是结构性的注意力优势。

**验证（三路搜索全部为空）**：
```
=== ContextualToolFilter (OPT-50) usage ===    ← 零命中（死代码）
=== Any tool filtering / dynamic tool selection mechanism? ===  ← 零命中（不存在）
=== run_mcp / meta-tool pattern (Claude Code style)? ===  ← 零命中（不存在）
```

**修复方向**：
1. 引入 `run_mcp({ server_name, tool_name, args })` 元工具，MCP 工具不再注册为顶层工具
2. 或实现真正的 ContextualToolFilter：按任务相关性每轮动态选择 MCP 工具子集
3. 当前 OPT-50 ContextualToolFilter 是死代码（仅 `GetStats()` 被调用），需激活或重写

---

### P0-3：OPT v2/v3 模块系统性浅集成

**证据（基础设施审计子代理验证）**：

`internal/agent/` 含 408 个 .go 文件，其中 OPT-XX 系列 token 优化模块存在系统性"浅集成"模式：模块有完整实现 + 完整测试，但核心功能方法从未接入 agent 主循环，仅 `GetStats()` 遥测被调用。

**16+ 个 v2/v3 死代码模块**（节选）：

| 文件 | 核心方法 | 生产调用 |
|------|----------|----------|
| [cache_warming_v2.go](file:///D:/开发/reasonix-src/internal/agent/cache_warming_v2.go) | LearnPattern/PredictFollowUp/WarmCache | **0** |
| [token_aware_compressor_v2.go](file:///D:/开发/reasonix-src/internal/agent/token_aware_compressor_v2.go) | Compress() | **0** |
| [cache_admission_controller_v2.go](file:///D:/开发/reasonix-src/internal/agent/cache_admission_controller_v2.go) | NewCacheAdmissionControllerV2 构造函数 | **0**（完全死代码） |
| [context_relevance_scorer_v2.go](file:///D:/开发/reasonix-src/internal/agent/context_relevance_scorer_v2.go) | 全部 | 仅 GetStats() |
| [token_aware_scheduler_v2.go](file:///D:/开发/reasonix-src/internal/agent/token_aware_scheduler_v2.go) | 全部 | 仅 GetStats() |
| ... 另有 11 个同类 | | |

**实例化与赋值位置**：
- [agent.go:1896-2101](file:///D:/开发/reasonix-src/internal/agent/agent.go#L1896-L2101) — 200+ 局部变量创建
- [agent.go:2103-2400](file:///D:/开发/reasonix-src/internal/agent/agent.go#L2103-L2400) — 赋值到 Agent 结构体字段
- [agent.go:2711-3094](file:///D:/开发/reasonix-src/internal/agent/agent.go#L2711-L3094) — 仅 `GetStats()` 收集指标

**注意**：v1 OPT 模块（如 `cacheEnforcer`）**并非全部死代码**——`cacheEnforcer` 在 [agent.go:3480-3483](file:///D:/开发/reasonix-src/internal/agent/agent.go#L3480-L3483) 的 Run() 中被真实调用。死代码集中在 v2/v3 升级版。这是"升级即废弃"模式：写了 v2/v3 替代品但从未切换调用链。

**对比**：OPT-05（Schema 压缩）和 OPT-08（描述沙箱化）**是真实激活的**，见 [plugin.go:1176-1179](file:///D:/开发/reasonix-src/internal/plugin/plugin.go#L1176-L1179)。说明 OPT 系统并非全部废弃，而是 v2/v3 升级版批量未接入。

---

### P0-4：SkillActivationCache 幽灵实现

**证据（三路审计独立确认）**：

[activation_cache.go](file:///D:/开发/reasonix-src/internal/skill/activation_cache.go)（OPT-35 技能激活缓存）：

- [L137-139](file:///D:/开发/reasonix-src/internal/skill/activation_cache.go#L137-L139) `saveToDisk()` 是**空函数**，仅注释"简化版：实际实现会写入 JSON 文件"
- [L130-134](file:///D:/开发/reasonix-src/internal/skill/activation_cache.go#L130-L134) `loadFromDisk()` 只 `MkdirAll`，**根本不加载**
- [L114-121](file:///D:/开发/reasonix-src/internal/skill/activation_cache.go#L114-L121) `hashWorkspace()` 仅用根目录路径+顶层 ModTime，子目录变化无法感知

**生产调用链验证**：
```
=== SkillActivationCache usage (non-test, non-definition) ===   ← 零命中
=== GetActiveSkills / CacheActiveSkills call sites ===           ← 零命中
```

**后果**：
1. 缓存从不持久化——重启后 `entries` map 为空
2. `GetActiveSkills()` 每次重启必 miss，声称的"O(n×m) → O(1)"优化不存在
3. 整个 OPT-35 模块是纯内存玩具，注释声称的"减少 500-1000 token"是纸面数字
4. Agent struct 中对应的 `skillActivationCache` 字段（OPT 字段之一）仅 `GetStats()` 被调用

---

## 三、P1 严重问题详解

### P1-5：WebSearch 工具缺失

Reasonix 内置 19 个工具，无 WebSearch。用户需通过 WebFetch 间接获取网页内容，无法做搜索查询。Claude Code 和 Codex CLI 均内置 WebSearch。

### P1-6：并行子代理仅限只读

[parallel_tasks.go](file:///D:/开发/reasonix-src/internal/agent/parallel_tasks.go) 仅支持只读并行。`task` 和 `read_only_task` 工具存在，但无法并行写子代理。Claude Code 支持并行写子代理 + 跨子代理通信 + 独立沙箱。

### P1-7：Plan Mode 模型不可控

Reasonix Plan Mode 通过 [input.go:28](file:///D:/开发/reasonix-src/internal/control/input.go#L28) 的 `PlanModeMarker` 注入用户回合实现，由前端/用户切换。无 `EnterPlanMode`/`ExitPlanMode` 工具供模型主动调用。Claude Code 模型可主动进入 Plan Mode 探索后再退出。

### P1-8：TodoWrite 字段缺失

[todo.go](file:///D:/开发/reasonix-src/internal/tool/builtin/todo.go) 的 Todo 结构只有 `content/status/activeForm/level`，缺 `id`（稳定标识）、`priority`（优先级）、`summary`（完成摘要）、`merge`（合并更新）。Claude Code Tasks v2 支持这些字段，支持并行任务管理与子任务合并。

### P1-9：MCP cache-miss 两轮握手

[lazy.go:259-276](file:///D:/开发/reasonix-src/internal/plugin/lazy.go#L259-L276)：无缓存 schema 时，注册单个 `mcp__<server>__connect` stub。模型调用后返回"is initializing — call again on the next turn"，浪费一整轮。Claude Code 的 run_mcp 元工具单轮完成发现+调用。

---

## 四、P2 中危问题详解

### P2-10：MCP cache pinning 会话内不可见变更

[lazy.go:7-14](file:///D:/开发/reasonix-src/internal/plugin/lazy.go#L7-L14)：cache-hit 占位符在会话内 PINNED，避免 provider prefix cache 失效（10x miss 定价）。代价：MCP 服务器会话内新增工具不可见，直到下个 session。这是为缓存稳定性的刻意权衡，但元工具方案无此限制。

### P2-11：Hooks 缺 prompt 类型

[hook.go](file:///D:/开发/reasonix-src/internal/hook/hook.go) 仅支持 command 类型 Hook（执行命令）。Claude Code 支持 prompt 类型 Hook（生成文本注入 prompt）。`HookConfig` 仅有 `Command` 和 `ContextFile` 字段。

**Reasonix 优势**：Hooks 事件类型更丰富——PermissionRequest、PostLLMCall、SessionStart/SessionEnd 是 Claude Code 没有的。

### P2-12：缺 CLI --resume 标志

[resume.go](file:///D:/开发/reasonix-src/internal/cli/resume.go) 仅支持 in-chat `/resume` 命令。无 CLI 级 `--resume <session-id>` 标志，无法从命令行直接恢复指定会话。

### P2-13：SSE 传输不一致

[plugin.go:985](file:///D:/开发/reasonix-src/internal/plugin/plugin.go#L985) 返回 "legacy sse transport not yet supported"，但 [cli/mcp.go:77-85](file:///D:/开发/reasonix-src/internal/cli/mcp.go#L77-L85) CLI 接受 `--sse` 标志。用户配置 `--sse` 会在运行时报错指向 `--http`。

### P2-14：Skill 索引盲截断

[index.go:62-64](file:///D:/开发/reasonix-src/internal/skill/index.go#L62-L64)：超出 `IndexMaxChars=16000` 时按字母序盲截断，不按相关性。60+ skill 时末尾 skill 不可见。此前已从 4000 提升至 16000（[index.go:13-18](file:///D:/开发/reasonix-src/internal/skill/index.go#L13-L18) 注释记录），但截断策略本身未改进。

### P2-15：review_gate 简化版实现

[review_gate.go:371](file:///D:/开发/reasonix-src/internal/agent/review_gate.go#L371) glob 匹配"简化版"；[review_gate.go:747](file:///D:/开发/reasonix-src/internal/agent/review_gate.go#L747) 签名生成"简化版：使用内容哈希"。

---

## 五、P3 结构问题

### P3-16 / P3-17：God files

| 文件 | 大小 |
|------|------|
| [desktop/app.go](file:///D:/开发/reasonix-src/desktop/app.go) | 275KB |
| [internal/agent/agent.go](file:///D:/开发/reasonix-src/internal/agent/agent.go) | 248KB（含 200+ OPT 字段） |
| [desktop/tabs.go](file:///D:/开发/reasonix-src/desktop/tabs.go) | 244KB |
| [internal/control/controller.go](file:///D:/开发/reasonix-src/internal/control/controller.go) | 186KB |
| [internal/cli/chat_tui.go](file:///D:/开发/reasonix-src/internal/cli/chat_tui.go) | 143KB |

`internal/agent/` 含 408 个 .go 文件，需按子系统拆分。详见 [AGENT_REFACTOR_PLAN.md](file:///D:/开发/reasonix-src/AGENT_REFACTOR_PLAN.md) 的 7 步重构计划（OPT Registry 是最高杠杆）。

### P3-18：Skill body 预加载矛盾

[skill.go:722](file:///D:/开发/reasonix-src/internal/skill/skill.go#L722) 在发现时即 `loadBodyWithScripts(loadBodyWithReferences(...))` 预加载 body（含 references/*.md 和 scripts/ 列表）。但 [index.go:6-7](file:///D:/开发/reasonix-src/internal/skill/index.go#L6-L7) 注释声称"bodies load on demand via run_skill"。实际 body 已在内存，run_skill 只读内存字段。47+ skill 时全部 body 常驻内存。

---

## 六、Reasonix 的优势（对标中确认）

审计中也发现 Reasonix **领先** Claude Code 的方面，应保留：

| 子系统 | 优势 | 证据 |
|--------|------|------|
| Hooks | 事件类型更丰富：PermissionRequest、PostLLMCall、SessionStart/SessionEnd | Claude Code 无这三种 |
| 沙箱 | Windows 原生实现完整：AppContainer + Low Integrity Token + ACL + 并发协调 | [winsandbox/](file:///D:/开发/reasonix-src/internal/winsandbox/) 43KB+22KB |
| MCP 懒加载 | cache pinning 设计优秀，会话内固定避免 provider 缓存 10x miss | [lazy.go](file:///D:/开发/reasonix-src/internal/plugin/lazy.go) |
| 检查点 | git-free 双重回退（对话 MsgIndex + 工作区 FileSnap） | [checkpoint.go](file:///D:/开发/reasonix-src/internal/checkpoint/checkpoint.go) |
| MCP 安全 | 工具描述沙箱化（OPT-08）+ Schema 压缩（OPT-05）真实激活 | [plugin.go:1176-1179](file:///D:/开发/reasonix-src/internal/plugin/plugin.go#L1176-L1179) |
| Memory | tail 注入持续到下个 session（已修复"死按旧文档"问题） | [control/memory.go:69-75](file:///D:/开发/reasonix-src/internal/control/memory.go#L69-L75) |

---

## 七、修复优先级建议

### 第一优先级：解决"AI 注意力"（P0-1/P0-2）

引入 `run_mcp` 元工具，MCP 工具不再注册为顶层工具。这是用户"MCP 注意力限制"投诉的结构性根因。预计效果：tools 数组从 100+ 降至 ~25，模型注意力集中度显著提升。

### 第二优先级：清理死代码（P0-3/P0-4）

执行 [AGENT_REFACTOR_PLAN.md](file:///D:/开发/reasonix-src/AGENT_REFACTOR_PLAN.md) 步骤 2（OPT Registry），将 249 个 OPT 字段合并为 1 个 registry。同时删除/激活 SkillActivationCache。预计效果：agent.go 5566 → ~400 行，struct 328 → ~23 字段。

### 第三优先级：补齐工具能力（P1-5/P1-7/P1-8）

- 新增 WebSearch 工具
- 新增 EnterPlanMode/ExitPlanMode 工具，让模型可控 Plan Mode
- TodoWrite 补 id/priority/summary/merge 字段，对齐 Claude Code Tasks v2

### 第四优先级：并行写子代理（P1-6）

扩展 parallel_tasks 支持写子代理 + 跨子代理通信。

### 第五优先级：中危收尾（P2）

MCP 单轮发现、prompt 类型 Hook、CLI --resume、SSE 一致性、Skill 索引相关性排序、review_gate 完整实现。

---

## 八、审计方法学备注

### 8.1 死代码判定标准

一个模块被判为死代码需同时满足：
1. 核心功能方法（非 GetStats/Get*）在生产代码（非 _test.go）中 0 引用
2. 构造函数可能被调用（实例化），但实例的方法不被调用
3. 测试覆盖完整（`opt*_test.go` 存在且通过）

此模式称为"幽灵实例化"（ghost instantiation）：代码创建对象并存储，使功能看似已实现并集成，但生产路径从不调用其方法。

### 8.2 验证工具

- PowerShell `Select-String` 全树搜索（注意 `$_` 在 `powershell -Command` 中会被 shell 转义，需用脚本文件或 `Select-Object -ExpandProperty`）
- 三路并行审计交叉验证：agent 核心（前序）+ 基础设施（子代理 4679e544）+ skill/MCP（本次）
- 关键发现（如 SkillActivationCache）被两路独立审计同时确认

### 8.3 未覆盖范围

本次审计未深入：
- `desktop/` 前端层（app.go 275KB God file 需单独审计）
- `internal/autoresearch/`（AutoResearch 系统）
- `internal/capability/` + `internal/capdiag/`（能力账本）
- `internal/evidence/`（证据系统）
- `internal/billing/`（计费）
- `internal/lsp/`（LSP 集成）

建议后续按子系统继续审计。
