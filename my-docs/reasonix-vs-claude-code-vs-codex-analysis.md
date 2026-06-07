# Reasonix vs Claude Code CLI vs Codex — 深度对比分析报告

**报告日期**: 2026-06-07  
**分析版本**: Reasonix (DeepSeek-backed fork)，Claude Code CLI (Anthropic)，Codex CLI (OpenAI)  
**项目代码根目录**: `internal/` 共 ~495 个 Go 源文件  
**报告存储路径**: `my-docs/`（非官方文档，不干扰项目自身文档）

---

## 目录

1. [三个工具定位概览](#1-三个工具定位概览)
2. [核心架构对比](#2-核心架构对比)
3. [关键维度逐项对比](#3-关键维度逐项对比)
   - 3.1 [LLM 提供商灵活性](#31-llm-提供商灵活性)
   - 3.2 [前端多样性](#32-前端多样性)
   - 3.3 [工具系统与文件操作](#33-工具系统与文件操作)
   - 3.4 [上下文管理与缓存策略](#34-上下文管理与缓存策略)
   - 3.5 [代码智能 (Code Intelligence)](#35-代码智能-code-intelligence)
   - 3.6 [插件与 MCP](#36-插件与-mcp)
   - 3.7 [记忆系统](#37-记忆系统)
   - 3.8 [安全沙箱与权限](#38-安全沙箱与权限)
   - 3.9 [证据与验证 (Evidence)](#39-证据与验证-evidence)
   - 3.10 [多模型协作 (Coordinator)](#310-多模型协作-coordinator)
   - 3.11 [回环检测与防护](#311-回环检测与防护)
   - 3.12 [企业级部署能力](#312-企业级部署能力)
4. [优势对比总结](#4-优势对比总结)
5. [Reasonix 的劣势与潜在风险](#5-reasonix-的劣势与潜在风险)
6. [综合评价](#6-综合评价)

---

## 1. 三个工具定位概览

| 维度 | Reasonix | Claude Code CLI | Codex CLI |
|:-----|:---------|:----------------|:----------|
| **开发者** | 社区（DeepSeek 生态） | Anthropic | OpenAI |
| **核心语言** | Go | TypeScript/Node.js | Rust |
| **开源协议** | Apache 2.0 | 专有 | Apache 2.0 |
| **LLM 后端** | **可插拔**（OpenAI、Anthropic、DeepSeek 等） | **仅 Anthropic Claude** | **仅 OpenAI**（需 ChatGPT 订阅或 API Key） |
| **主要形态** | CLI TUI + HTTP/SSE Serve + Wails 桌面应用 | CLI + VS Code 插件 + 桌面应用 + Web + JetBrains | CLI 终端 + IDE 插件 + 桌面应用 + Web |
| **亮点** | 提供软件包作为库，多提供商支持，丰富的内置工具 | 成熟的商业模式，企业级管理能力，Codex 生态集成 | OpenAI 官方，Rust 性能，ChatGPT 订阅可用 |
| **Star/社区** | 社区规模相对较小 | 企业级用户群庞大 | 89k+ stars，快速增长 |

---

## 2. 核心架构对比

```
Reasonix:
                    ┌──────────────┐
                    │   Frontends  │
                    │  TUI / HTTP  │
                    │  Desktop/ACP │
                    └──────┬───────┘
                           │ event.Sink
                    ┌──────▼───────┐
                    │  Controller  │  ← 单一命令面
                    └──────┬───────┘
              ┌────────────┼────────────┐
         ┌────▼───┐  ┌────▼───┐  ┌────▼────┐
         │  Agent  │  │Coord.  │  │  Jobs   │
         │(单模型) │  │(双模型) │  │(后台)   │
         └────┬───┘  └────┬───┘  └─────────┘
    ┌─────────┼───────────┼──────────┐
    │    Provider (可插拔)            │
    │  OpenAI / Anthropic / DeepSeek  │
    │         + tool.Registry         │
    └─────────────────────────────────┘

Claude Code CLI:
                    ┌──────────────┐
                    │   Frontends  │
                    │  CLI / VS Code│
                    │  Desktop/Web  │
                    │  JetBrains    │
                    └──────┬───────┘
                    ┌──────▼───────┐
                    │   Claude     │
                    │   Code Core  │
                    │  (闭源)      │
                    └──────┬───────┘
                    ┌──────▼───────┐
                    │  Anthropic   │
                    │    API       │
                    │  (仅Claude)  │
                    └──────────────┘

Codex CLI:
                    ┌──────────────┐
                    │   Frontends  │
                    │  CLI / IDE   │
                    │  Desktop/Web │
                    └──────┬───────┘
                    ┌──────▼───────┐
                    │  Codex Core  │
                    │   (Rust)     │
                    └──────┬───────┘
                    ┌──────▼───────┐
                    │  OpenAI API  │
                    │  (仅OpenAI)  │
                    └──────────────┘
```

---

## 3. 关键维度逐项对比

### 3.1 LLM 提供商灵活性

| 工具 | 支持多家 LLM | 切换方式 | 分析 |
|:-----|:-----------|:--------|:-----|
| **Reasonix** | ✅ **是** — OpenAI, Anthropic, DeepSeek 等 | `reasonix.toml` 中配置 provider + 运行时 /model 切换 | **核心差异化优势**。`provider.Factory` 注册机制使新增提供商只需实现 `Stream()` 接口 + `init()` 注册。`SanitizeToolPairing` 统一处理不同 API 的差异（如 DeepSeek 和 Anthropic 的缓存 token 计量标准不同） |
| **Claude Code CLI** | ❌ 仅 Anthropic Claude | 在 sonnet/haiku/opus 间切换 | 垂直集成，针对 Claude 的 prompt 格式深度优化。Thinking mode（扩展思考）是专有功能 |
| **Codex CLI** | ❌ 仅 OpenAI | 使用 ChatGPT 订阅或 API Key | 仅支持 OpenAI 模型 |

**Reasonix 优势**: 真正的中立层——同一套工具、同一套工作流可以支持不同的 LLM 后端。这在企业希望跨模型迁移时价值巨大。

**Reasonix 劣势**: 对每个提供商都需要适配（token 计算、API 差异、think 模式等），维护成本高。`internal/provider/` 中 Anthropic 和 OpenAI 的实现在细节上有大量条件分支。

---

### 3.2 前端多样性

| 工具 | CLI/TUI | VS Code | Web | 桌面应用 | IDE 插件 | 协议层 |
|:-----|:--------|:--------|:----|:--------|:--------|:------|
| **Reasonix** | ✅ 原生 TUI（Bubbletea） | ❌ 无（可自建 HTTP 对接） | ✅ HTTP/SSE Serve | ✅ Wails 桌面 | ❌ | ✅ ACP v1 (JSON-RPC stdio) |
| **Claude Code CLI** | ✅ 终端 CLI | ✅ 官方 VS Code 插件 | ✅ code.claude.ai | ✅ 桌面应用 | ✅ JetBrains | ✅ Remote Control + Channels |
| **Codex CLI** | ✅ 终端 CLI | ✅ VS Code/Cursor/Windsurf | ✅ chatgpt.com/codex | ✅ 桌面应用 | ❌ | ❌ 未公开 |

**Reasonix 优势**: 
- 所有前端共享同一个 `control.Controller` + `boot.Build()`，**行为完全一致**。Claude Code 的不同前端可能由不同团队维护，行为可能有细微差异。
- ACP 协议层是开放标准，允许第三方客户端接入。

**Reasonix 劣势**:
- 前端覆盖面少，缺少 VS Code 插件、JetBrains 插件等开发者日常使用的 IDE 集成。
- Wails 桌面应用功能基础（多 tab 是亮点），但 UI 精细度不如 Claude Code Desktop。

---

### 3.3 工具系统与文件操作

| 工具 | 工具实现 | 并行执行 | 只读分类 | 差异预览 |
|:-----|:--------|:--------|:--------|:--------|
| **Reasonix** | Go 原生，`RegisterBuiltin` 自注册 | ✅ 并行只读，串行写入 | ✅ 静态 `ReadOnly()` 标记 | ✅ `Previewer` 接口 |
| **Claude Code CLI** | Node.js/TypeScript | ✅ 类似 | ✅ 类似 | ✅ 类似 |
| **Codex CLI** | Rust | 部分 | 部分 | 部分 |

**Reasonix 优势**:
- **`Previewer` 接口**：在写入工具执行前允许系统检查变更内容，这个设计支持了 checkpoint 快照和权限审批卡片。
- **confine 机制**：限制 bash 在 workspace 内，不允许脱离项目目录，比 Claude Code 的权限规则更底层。
- **storm breaker** 和 **repeat-success blocker**：两层互补的回环保护，这在其他两个工具中未见公开文档。

**Reasonix 劣势**:
- 工具数量庞大但文档分散。Claude Code 有完善的 settings.json + 权限规则文档。
- 工具执行缺乏超时粒度的分层（Claude Code 有 bash timeout、per-tool timeout 等）。

---

### 3.4 上下文管理与缓存策略

| 工具 | 缓存策略 | 压缩机制 | 分析 |
|:-----|:--------|:--------|:-----|
| **Reasonix** | **缓存稳定前缀**：system prompt 跨轮次保持不变 | token-budgeted 压缩（三阈值：软/触发/强制）+ compactStuck 锁 | **核心创新**：system prompt 永远不会改变——plan mode 是运行时布尔值而不是 prompt 编辑。mid-session memory 更改走 turn-tail 注入而非修改前缀 |
| **Claude Code CLI** | Anthropic 端 prompt caching | 内部压缩（闭源） | 依赖 Anthropic 的缓存 API，用户不可控 |
| **Codex CLI** | OpenAI 端 prompt caching | 内部压缩（闭源） | 与 Claude Code 类似，依赖 API 端缓存 |

**Reasonix 优势**:
- **缓存稳定前缀是架构级设计**，而 Claude Code 和 Codex 的缓存是 API 端黑盒。
- **Token-budgeted 尾部保留**：不是按消息数量保留，而是按 token 预算——这防止了大工具输出后每轮都触发压缩。
- **`compactStuck` 锁**：当 system prompt + 一轮对话已经超过上下文窗口时停止压缩，避免无意义地反复压缩。
- **tok_per_char 来自实际 provider 使用数据**，而非硬编码 tokenizer。

**Reasonix 劣势**:
- 压缩策略配置复杂（三个比率 + 两个保留量），对普通用户不够友好。
- 需要用户理解上下文窗口的概念才能调优。

---

### 3.5 代码智能 (Code Intelligence)

| 工具 | 静态分析 | LSP 集成 | 分析 |
|:-----|:---------|:--------|:-----|
| **Reasonix** | ✅ CodeGraph (tree-sitter + SQLite FTS5)，**内置 MCP 服务器** | ✅ 内置，支持 14 种语言 | **单二进制包含完整代码智能**，首次使用时按需下载引擎 |
| **Claude Code CLI** | 依赖 Claude 的内在能力 | ✅ 已集成 | Codex 的代码理解依赖模型本身 |
| **Codex CLI** | 未公开 | 未公开 | 代码理解依赖模型本身 |

**Reasonix 优势**:
- **CodeGraph 是独立 MCP 服务器**，不是模型能力的一部分。这意味着它提供的是确定性的符号分析（调用图、引用查找、影响分析），而不仅靠模型猜测。
- **`codegraph_context` 是一个综合入口**：一次调用返回入口点 + 相关符号 + 核心代码，专为减少模型上下文消耗而设计。
- **LSP 集成有 14 种语言的内置定义**，且支持通过配置添加新语言。

**Reasonix 劣势**:
- CodeGraph 索引需要时间（~100ms 创建 + 异步填充），首次使用时对大型项目可能需要等待。
- Claude Code 的代码理解与模型深度整合（如 "use claude" 的代码库感知），Reasonix 在这方面需要更多 prompt 引导（`SteerText`）。

---

### 3.6 插件与 MCP

| 工具 | MCP 支持 | 插件体系 | MCP 服务器管理 | 分析 |
|:-----|:--------|:--------|:--------------|:-----|
| **Reasonix** | ✅ 完整支持（stdio/HTTP SSE/Streamable HTTP） | 有限（plugin 层） | **启动层级**：eager/lazy/background + 自动降级 | 可编程的 MCP 管理（热添加/移除） |
| **Claude Code CLI** | ✅ 完整 MCP 支持 | ✅ 完整的插件市场体系 | 项目级 `.mcp.json` + 用户级 `~/.claude.json` | 有插件市场、插件依赖版本管理 |
| **Codex CLI** | 未公开 | 未公开 | 未公开 | —

**Reasonix 优势**:
- **MCP 启动层级**：eager（阻塞、必须启动成功）→ lazy（按需）→ background（异步），这是 Claude Code 没有的分层策略。
- **自动降级**：如果某个 MCP 服务器多次启动缓慢，系统会自动将其从 eager 降级为 lazy——这是很实用的弹性设计。
- **MCP 工具名命名空间**：`mcp__<server>__<tool>`，防止不同服务器的工具名冲突。

**Reasonix 劣势**:
- **没有插件市场**。Claude Code 有完整的插件市场体系（`plugins.json` + 依赖版本管理 + 企业级 marketplace 限制），Reasonix 在这方面基本空白。
- MCP 配置方式不如 Claude Code 友好（Claude Code 有 `managed-mcp.json` 支持企业统一部署）。

---

### 3.7 记忆系统

| 工具 | 文档记忆 | 自动记忆 | 记忆管理工具 | 分析 |
|:-----|:--------|:--------|:-----------|:-----|
| **Reasonix** | ✅ 层级 Markdown（项目/本地/用户） | ✅ 按类型分类（user/feedback/project/reference） | ✅ `remember`/`forget` 工具 | 自动记忆有 MEMORY.md 索引文件 |
| **Claude Code CLI** | ✅ CLAUDE.md | ✅ Auto memory | ✅ `remember`/`forget` 工具 | 自动记忆存储于 `--auto-memory-directory` |
| **Codex CLI** | ✅ AGENTS.md | 未公开 | 未公开 | —

**Reasonix 优势**:
- **层级记忆系统**（项目级 + 本地 + 用户级），Claude Code 也有类似但概念不同。
- 自动记忆支持**四种记忆类型**（user/feedback/project/reference），按类型管理更清晰。
- **`quickadd` 工具**：`#<note>` 语法快速追加到 REASONIX.md，无需打开文件编辑。

**Reasonix 劣势**:
- 与 Claude Code 类似，无显著差异。Codex 的记忆系统目前不透明，无法深入对比。

---

### 3.8 安全沙箱与权限

| 工具 | 沙箱 | 权限规则 | 审批流程 | 分析 |
|:-----|:-----|:--------|:--------|:-----|
| **Reasonix** | ✅ macOS Seatbelt | ✅ allow/deny/ask 规则 | ✅ 交互式审批 + YOLO 模式 | **多模型协作审批** |
| **Claude Code CLI** | ✅ 多种沙箱（Docker/dev container/Seatbelt/Bubblewrap） | ✅ 四层 scope (managed/user/project/local) | ✅ 交互式审批 + 多种权限模式 | **企业级管理策略** |
| **Codex CLI** | 未公开 | 未公开 | 未公开 | —

**Reasonix 优势**:
- `confine` 工具限制了 bash 只能在 workspace 内执行，这是代码层面的约束而非配置规则。
- Seatbelt 沙箱 + confine + permission policy 构成**三层防护**（OS 级 → 工作目录级 → 用户配置级）。

**Reasonix 劣势**:
- **Claude Code 的沙箱选择更多**：Docker、dev container、Bubblewrap（Linux），覆盖了 macOS 之外的平台。
- Claude Code 的 **managed settings** 支持企业 IT 通过 MDM（Jamf/Intune/Group Policy）统一部署安全策略——Reasonix 完全没有这个能力。
- Reasonix 只有 macOS 沙箱，Linux/Windows 上只有 confine 和 policy，安全层较薄。

---

### 3.9 证据与验证 (Evidence)

| 工具 | 证据系统 | 验证周期 | 分析 |
|:-----|:--------|:--------|:-----|
| **Reasonix** | ✅ **`evidence.Ledger`** 每轮工具调用的凭证账簿 | ✅ `complete_step` 交叉验证工具输出与模型声明 | **独特创新**，其他工具未见 |
| **Claude Code CLI** | ❌ 无 | ❌ 无 | —
| **Codex CLI** | ❌ 无 | ❌ 无 | —

**Reasonix 优势**（独家）:
- **`evidence.Ledger`** 是一个每轮重置的工具调用凭证记录器。`complete_step` 在签字确认时，会**交叉验证模型的声明与实际工具输出**。
- 例如：模型说"我修改了文件 X"，`complete_step` 会检查 ledger 中是否真的有一个成功的写入工具操作了文件 X。如果不匹配，**步骤被拒绝**。
- 这是防止模型幻觉的架构级设计——Claude Code 和 Codex CLI 都依赖模型自我声明，没有独立验证层。

---

### 3.10 多模型协作 (Coordinator)

| 工具 | 双模型模式 | 规划器隔离 | 分析 |
|:-----|:----------|:---------|:-----|
| **Reasonix** | ✅ **`Coordinator`** — planner + executor | ✅ 独立 session、独立缓存前缀、只读工具 | **独特创新** |
| **Claude Code CLI** | ✅ `opusplan` 类似 | 闭源实现 | —
| **Codex CLI** | ❌ 未公开 | ❌ | —

**Reasonix 优势**:
- **Coordinator 中的规划器拥有独立的 session、独立缓存前缀、独立 max-steps**。这意味着规划器的上下文不会与执行器的混合。
- `shouldPlan` 过滤器能自动跳过问候/简单问题，避免不必要的规划开销。
- **handoff 格式**简洁且明确："Task: … A planner proposed this approach: … Carry it out, adapting as needed."——向执行器传递的是结构化指令而不是原始的思维链。

**Reasonix 劣势**:
- Claude Code 的 `opusplan` 已经形成用户认知，"用便宜模型规划，用强模型执行"是成熟的工作流。Reasonix 需要更多文档来引导用户使用这个功能。

---

### 3.11 回环检测与防护

| 工具 | 风暴检测器 | 重复成功阻断 | 分析 |
|:-----|:----------|:-----------|:-----|
| **Reasonix** | ✅ **storm breaker** | ✅ **repeat-success blocker** | **独特创新** |
| **Claude Code CLI** | ❌ 未公开 | ❌ 未公开 | —
| **Codex CLI** | ❌ 未公开 | ❌ 未公开 | —

**Reasonix 优势**（独家）:
- **Storm breaker**：如果 3 次连续出现相同的 (tool, error) 签名，注入回环干预消息。这捕获了即使参数变化但错误模式不变的回环。
- **Repeat-success blocker**：如果同一个写工具在同一个用户轮次中以完全相同参数成功 3 次，直接阻断。
- 两个机制互补：一个针对错误回环，一个针对成功回环——**同时覆盖了失败和成功的无限循环**。

---

### 3.12 企业级部署能力

| 工具 | 企业配置管理 | MDM/组策略 | 使用分析 | 数据保留策略 |
|:-----|:-----------|:---------|:--------|:----------|
| **Claude Code CLI** | ✅ managed-settings.json 四层覆盖 | ✅ Jamf/Intune/Group Policy | ✅ 分析仪表盘 | ✅ Zero Data Retention |
| **Reasonix** | ❌ 只有项目级/用户级 toml | ❌ 无 | ❌ 无 | ❌ 无 |
| **Codex CLI** | ❌ | ❌ | ❌ | ❌ |

**Reasonix 在这一维度完全落后于 Claude Code CLI**。Claude Code 投入了大量工程在企业部署能力上：托管设置、MDM 集成、使用分析仪表盘、Zero Data Retention。Reasonix 和 Codex CLI 目前都缺乏这些能力。

---

## 4. 优势对比总结

### ✅ Reasonix 显著领先的领域

| # | 优势 | 详情 | 独家程度 |
|:--|:-----|:------|:--------|
| 1 | **LLM 提供商可插拔** | 支持 OpenAI/Anthropic/DeepSeek 多后端，统一工具接口 | **独家** — Claude Code/Codex 均绑定自家模型 |
| 2 | **证据验证系统 (Evidence)** | 每轮工具调用 ledger + `complete_step` 交叉验证，防幻觉 | **独家** — 业界首创 |
| 3 | **缓存稳定前缀架构** | System prompt 从不改变，plan mode 是运行时布尔值，memory 走 turn-tail 注入 | **独家** — Claude Code/Codex 依赖 API 端缓存 |
| 4 | **回环防护（双保险）** | Storm breaker + repeat-success blocker 覆盖失败和成功两种回环 | **独家** |
| 5 | **CodeGraph 代码智能** | 树搜索器 + SQLite 确定性符号分析，不依赖模型猜测 | 部分独家（Claude 有类似但实现不同） |
| 6 | **Coordinator 双模型协作** | 规划器有独立 session/缓存/工具集 | 部分独家（Claude 的 opusplan 类似但闭源） |
| 7 | **MCP启动层级** | eager/lazy/background 三级 + 自动降级 | **独家** |
| 8 | **前端共享 Controller** | TUI/HTTP/Desktop/ACP 四种前端行为完全一致 | 差异化的架构选择 |
| 9 | **作为 Go 库提供** | 可以通过 SDK 集成到其他 Go 应用中 | 显著优势 |

### ✅ Claude Code CLI 显著领先的领域

| # | 优势 | 详情 |
|:--|:-----|:------|
| 1 | **IDE 集成** | VS Code 插件 + JetBrains 插件 + inline diff + @-mentions |
| 2 | **企业部署** | managed-settings、MDM (Jamf/Intune)、分析仪表盘、ZDR |
| 3 | **插件市场** | 完整的插件生态系统、版本管理、渠道管理 |
| 4 | **多平台沙箱** | Docker、dev container、Bubblewrap、Seatbelt 多种选择 |
| 5 | **生态成熟度** | 文档完善、社区活跃、商业模式成熟 |
| 6 | **前端覆盖** | CLI + VS Code + Desktop + Web + JetBrains + Slack |
| 7 | **托管功能** | Routines（云端定时任务）、Remote Control（远程接管） |

### ✅ Codex CLI 的亮点

| # | 亮点 | 详情 |
|:--|:-----|:------|
| 1 | **Rust 实现性能** | 单二进制，启动快，资源占用低 |
| 2 | **ChatGPT 订阅可用** | 无需额外付费，Plus/Pro 用户可直接使用 |
| 3 | **GitHub 集成** | 89k+ stars，社区活跃度高 |
| 4 | **开源** | Apache 2.0 许可 |

---

## 5. Reasonix 的劣势与潜在风险

### 5.1 工程质量的待改进项

| 问题 | 严重程度 | 详情 |
|:-----|:--------|:------|
| **文档分散且不完整** | 🔴 高 | 很多关键功能（如 Coordinator、MCP 层级、Evidence 系统）没有面向用户的文档，需要读源码才知道 |
| **测试覆盖率参差不齐** | 🟡 中 | `internal/agent/` 测试丰富，但 `internal/tool/builtin/` 的部分工具有限（如 delete_range、multi_edit 的测试场景不够全面） |
| **配置复杂度高** | 🟡 中 | 三个压缩比率、两个保留量、provider 配置、沙箱配置——新用户上手曲线陡峭 |
| **错误信息不够友好** | 🟡 中 | 如前次测试所见，workflow 校验错误信息是机器可读但用户不友好的 |

### 5.2 架构风险

| 风险 | 严重程度 | 分析 |
|:-----|:--------|:-----|
| **多提供商适配维护成本** | 🟡 中 | 每个 LLM 提供商都有 API 差异（think token、message format、cache metrics），`internal/provider/` 中的条件分支会随着支持更多提供商而爆炸 |
| **单一二进制体积** | 🟡 中 | 内置 MCP 服务器下载 + LSP 支持 + Wails 桌面，二进制体积可能接近或超过 Claude Code |
| **桌面端依赖 Wails/WKWebView** | 🟢 低 | macOS 上依赖 WKWebView 渲染，前端 UI 的灵活性和一致性受限于 WebView |

### 5.3 生态差距

| 差距 | 严重程度 | 与竞品对比 |
|:-----|:--------|:----------|
| **无插件市场** | 🔴 高 | Claude Code 有完整插件市场，Codex 背靠 OpenAI 生态 |
| **缺少 IDE 插件** | 🔴 高 | 开发者日常主要使用 IDE，缺少 VS Code/JetBrains 插件是重大缺失 |
| **缺少企业级部署能力** | 🔴 高 | 无 managed settings、无 MDM、无使用分析 |
| **社区规模小** | 🟡 中 | 文档贡献者少，问题响应慢 |
| **缺少 SaaS/托管服务** | 🟡 中 | Claude Code 有 Routines（云端定时任务），Codex 有 ChatGPT 集成 |

### 5.4 代码库自身的风险点

| 风险点 | 分析 |
|:-------|:-----|
| `internal/provider/anthropic/` 和 `internal/provider/openai/` 有大量重复的模式匹配代码 | 需要对 provider 抽象层做更彻底的 interface 设计 |
| `internal/agent/agent.go` 是一个 ~500+ 行的巨型方法 `Run()` | 可测试性差，单测难以覆盖所有分支 |
| `internal/control/controller.go` 承担了过多职责 | session 管理、MCP 管理、记忆管理、审批流、checkpoint——违反了单一职责 |
| `internal/cli/chat_tui.go` 使用 Bubbletea + `tea.Println` 输出 | 这导致最终输出不进入 TUI 管理的区域，而直接写到原生 scrollback——可能在不同终端模拟器上行为不一致 |

---

## 6. 综合评价

### Reasonix 的独特价值

Reasonix 不是一个"另一个 Claude Code"，而是**一个架构思维完全不同的编码代理**。它的核心哲学是：

> **"做一个中立的代理框架，让 LLM 成为可替换的组件，而不是反过来。"**

在这个哲学下诞生的独特设计（Evidence 验证、缓存稳定前缀、双模型 Coordinator、CodeGraph MCP 服务器、回环双防护）在 Claude Code 和 Codex CLI 中都没有对应物。

### Reasonix 的最佳适用场景

| 场景 | 推荐度 | 原因 |
|:-----|:------|:-----|
| 需要使用 DeepSeek/开源模型的企业 | ⭐⭐⭐⭐⭐ | **唯一支持非 OpenAI/Anthropic 模型的生产级编码代理** |
| 高可靠性要求的代码操作 | ⭐⭐⭐⭐ | Evidence 验证系统 + 回环防护提供了其他工具没有的保障层 |
| 需要将编码代理作为库集成 | ⭐⭐⭐⭐ | Go 包可直接引用，`control.Controller` 是纯粹的库 API |
| 安全意识强的团队 | ⭐⭐⭐⭐ | 三层防护（Seatbelt + confine + policy） |
| 多前端统一体验 | ⭐⭐⭐⭐ | TUI/HTTP/Desktop 行为一致 |

### Reasonix 的薄弱场景

| 场景 | 推荐度 | 原因 |
|:-----|:------|:-----|
| 需要 IDE 深度集成 | ⭐⭐ | 没有 VS Code/JetBrains 插件 |
| 企业级大规模部署 | ⭐⭐ | 缺少 managed settings、MDM、分析仪表盘 |
| 新手上手 | ⭐⭐⭐ | 配置复杂、文档分散、错误信息不够友好 |
| 跨平台桌面体验 | ⭐⭐⭐ | Wails 桌面应用功能基础，不如 Claude Code Desktop 完善 |

---

### 与竞品的核心差异化总结

```
Reasonix 有 而 竞品 没有的：
├── 多 LLM 提供商支持（真正可切换）
├── Evidence 证据验证系统
├── 缓存稳定前缀架构
├── Storm Breaker + Repeat-Success Blocker 双环防护
├── MCP 启动层级 + 自动降级
├── CodeGraph 内建 MCP 服务器
├── Coordinato 独立 session 双模型模式
└── Go 库可直接引用

Claude Code 有 而 Reasonix 没有的：
├── VS Code / JetBrains 插件
├── 企业级 managed settings + MDM
├── 插件市场
├── 云端定时任务 (Routines)
├── 多平台沙箱（Docker / Bubblewrap）
├── 远程控制 (Remote Control)
└── 使用分析仪表盘

Codex 有 而 Reasonix 没有的：
├── ChatGPT 订阅直接可用
├── Rust 性能
└── OpenAI 原生生态集成
```

---

*本报告仅基于对 Reasonix 项目代码的理解和 Claude Code/Codex 公开文档撰写。竞品的内部实现细节基于公开资料推断，可能存在不准确之处。*
