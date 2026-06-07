# Reasonix 智能体能力评估报告（完整版）

**报告日期**: 2026-06-07  
**评估范围**: 编码智能体（Coding Agent）全部核心能力维度，不涉及商业运营/插件市场  
**评估方法**: 源代码分析（~19,000 行 Go 代码），覆盖全部 29 个内部包  
**对比基准**: Claude Code CLI（行业标杆）

---

## 快速总览

| 分类 | 评分 | 一句话概括 |
|:-----|:----:|:----------|
| **Agent 核心循环** | ★★★★★ | 可靠的主循环 + 回环防护 + 证据系统（独家） |
| **工具系统** | ★★★★☆ | 架构 5★，覆盖面 4★（缺 git/正则/视觉工具） |
| **LLM 集成** | ★★★★★ | 多提供商可插拔，开源模型唯一选择 |
| **代码智能** | ★★★★☆ | CodeGraph + LSP 14 语言，但缺代码补全 |
| **记忆系统** | ★★★★☆ | 层级文档 + 4 类自动记忆 |
| **技能/命令** | ★★★★☆ | Skill + 命令模板 + 子代理隔离 |
| **Hook 扩展** | ★★★★☆ | 10 事件点 + PostLLMCall 改写推理，但缺 HTTP/异步 |
| **MCP/插件** | ★★★★☆ | 3 传输 + 3 启动层 + 热管理，但缺市场/发现 |
| **安全/权限** | ★★★★☆ | 4 层防护，但 Linux 沙箱不完整 |
| **上下文管理** | ★★★★★ | 三阈值压缩 + 缓存稳定前缀（独家） |
| **会话管理** | ★★★★★ | JSONL 持久化 + 分支 + 检查点 + 跨进程恢复 |
| **前端覆盖** | ★★★☆☆ | TUI+HTTP+Desktop 三端，缺 IDE 插件 |
| **国际化/诊断** | ★★★★☆ | 完整 i18n + doctor 诊断 |
| **成本控制** | ★★★★☆ | 用量跟踪 + 余额查询 |
| **代码质量** | ★★★★☆ | 架构清晰，但核心文件过大 |

**综合评分: ★★★★☆ (成熟，有明确改进方向)**

---

## 第一章：能力完整度总览

### 编码智能体的全部能力域

我将一个编码智能体应具备的能力分为 7 大类、28 个子项：

| # | 能力域 | 子项 | Reasonix | Claude Code | 差距 |
|:--|:-------|:-----|:--------:|:-----------:|:----|
| | **1. 代码操作** | | | | |
| 1 | | 文件读写 | ✅ 完整 | ✅ | — |
| 2 | | 代码编辑（精确替换） | ✅ edit_file/multi_edit | ✅ | — |
| 3 | | **正则搜索替换** | ❌ | ❌ | 两个都缺 |
| 4 | | **Git 原生操作** | ❌ 仅 bash | ❌ 仅 bash | 两个都缺 |
| 5 | | 批量操作 | ⚠️ 无批量删除/重命名 | ⚠️ | — |
| 6 | | Jupyter 编辑 | ✅ notebook_edit | ❌ | **领先** |
| | **2. 代码智能** | | | | |
| 7 | | 符号搜索/引用 | ✅ LSP 4工具+CodeGraph 7工具 | ✅ | — |
| 8 | | 诊断/错误 | ✅ lsp_diagnostics | ✅ | — |
| 9 | | **代码补全** | ❌ 无 lsp_completion | ❌ | 两个都缺 |
| 10 | | 影响分析 | ✅ codegraph_impact | ⚠️ 依赖模型 | **领先** |
| | **3. 上下文管理** | | | | |
| 11 | | 会话持久化 | ✅ JSONL 原子写入 | ✅ | — |
| 12 | | 自动压缩 | ✅ 三阈值 token-budgeted | ✅ | — |
| 13 | | 提示缓存 | ✅ 缓存稳定前缀（架构级） | ✅ API 端 | **架构优势** |
| 14 | | 检查点/回退 | ✅ 文件+对话双回退 | ✅ | — |
| | **4. 安全控制** | | | | |
| 15 | | 权限规则 | ✅ deny/allow/ask 链 | ✅ 更细粒度 | — |
| 16 | | 沙箱隔离 | ⚠️ 仅 macOS Seatbelt | ✅ Docker+dev container | **落后** |
| 17 | | 审批流程 | ✅ 交互式审批 | ✅ | — |
| | **5. 模型与推理** | | | | |
| 18 | | 多模型切换 | ✅ 运行时切换 | ✅ | — |
| 19 | | **多提供商支持** | ✅ **OpenAI+Anthropic+DeepSeek** | ❌ 仅 Claude | **独家优势** |
| 20 | | 双模型协作 | ✅ Coordinator 独立 session | ✅ opusplan | — |
| 21 | | **视觉/多模态** | ❌ 无图片输入 | ✅ Claude 支持 | **落后** |
| | **6. 扩展性** | | | | |
| 22 | | MCP 协议 | ✅ 3 传输层 | ✅ | — |
| 23 | | Hook 脚本 | ✅ 10 事件点 | ✅ 更丰富 | — |
| 24 | | Skill 技能 | ✅ 多约定+子代理 | ✅ | — |
| 25 | | **插件市场** | ❌ | ✅ | **落后** |
| | **7. 可靠性** | | | | |
| 26 | | 回环防护 | ✅ **Storm+Repeat 双保险** | ❌ 未公开 | **独家优势** |
| 27 | | 证据验证 | ✅ **complete_step 交叉验证** | ❌ | **独家优势** |
| 28 | | 后台任务 | ✅ bgjobs + /loop | ✅ | — |

---

## 第二章：核心能力深度分析

### 2.1 Agent 核心循环 — ★★★★★

| 组件 | 状态 | 关键实现 |
|:-----|:----|:---------|
| 主循环 | ✅ | `agent.Run()` — 流式接收 → 并行只读/串行写入 → 结果回馈 |
| 风暴检测 | ✅ 独家 | 3 次相同 (tool, error) 签名触发中断 |
| 重复阻断 | ✅ 独家 | 3 次相同 (tool, args) 成功写入直接阻断 |
| 并行调度 | ✅ | ReadOnly=true 的工具批量并行执行 |
| 证据验证 | ✅ 独家 | `complete_step` 交叉验证模型声明 vs 工具输出 |
| 最终答案检查 | ✅ | ReadinessAudit 确保所有步骤已签字 |
| 子代理 | ✅ | `task` 工具 → 独立 session + 过滤工具集 |
| 后台任务 | ✅ | bgjobs 跨轮次存活，输出可后续读取 |
| 重试 | ✅ | provider/retry.go 带退避重试 |

**代码成熟度**: `agent.go` (1239行) 和 `controller.go` (~1900行) 承担了过多职责，是重构候选。

### 2.2 工具系统 — ★★★★☆

#### 已实现的 17 个内置工具

```
文件操作:  read_file, write_file, edit_file, multi_edit, delete_range, delete_symbol
目录搜索:  ls, glob, grep
执行:      bash, web_fetch
笔记本:    notebook_edit
元操作:    todo_write, complete_step
后台:      bash_output, wait_job, kill_shell
```

#### 架构优势
- ✅ ReadOnly 标记 → 并行执行
- ✅ Previewer 接口 → 写入前 diff 预览，支撑 checkpoint 和审批
- ✅ `confine` 限域 → bash 限制在 workspace 内
- ✅ `init()` 自注册 → 新增工具无需修改清单
- ✅ Schema 规范化 + 缓存

#### 工具覆盖面缺口

| 缺失工具 | 影响 | 替代方案 | 修复难度 |
|:---------|:-----|:--------|:--------|
| **Git 工具** | 🔴 高 — 模型不了解 git 操作结构，出错风险高 | bash 调用 git | 🟡 中 — 封装 git commit/diff/branch/PR 为结构化工具 |
| **正则搜索替换** | 🟡 中 — 不能做跨文件重构 | grep(读)+edit_file(逐个替换) | 🟢 低 — 类似 multi_edit 但支持正则 |
| **批量文件操作** | 🟢 低 — 多文件重命名/删除 | bash mv/rm | 🟢 低 |
| **图片/视觉输入** | 🟡 中 — 不能分析 UI 截图/流程图 | 无 | 🔴 高 — 需要多模态模型支持 |
| **结构化输出** | 🟡 中 — 不能保证模型返回合法 JSON | 模型 prompt 约束 | 🟡 中 — 需 provider 层支持 |

**关键缺口: 缺少 Git 原生工具**

当前 agent 通过 `bash` 执行 git 命令，这意味着:
- 模型必须自己构造正确的 git 命令字符串
- 权限系统只能看到 "bash" 不能看到 "git commit"
- 没有结构化输出 (diff stat, commit hash, branch list)
- git 错误信息直接回传模型，不经过格式化

建议新增工具:
- `git_status` — 工作区状态
- `git_diff` — 结构化 diff
- `git_commit` — 安全提交（可审批）
- `git_branch` — 分支管理
- `git_log` — 提交历史

### 2.3 LLM 提供商集成 — ★★★★★

| 后端 | 注册方式 | 状态 |
|:-----|:--------|:-----|
| OpenAI 兼容 | `provider.Register("openai", New)` | ✅ 完整，含 think token 处理 |
| Anthropic | `provider.Register("anthropic", New)` | ✅ 完整，含推理签名 |
| 自定义 | `provider.Register("custom", New)` | ✅ Factory 接口，约 100 行实现 |

**与竞品差异**:
- Claude Code: ❌ 仅 Anthropic
- Codex CLI: ❌ 仅 OpenAI
- **Reasonix: ✅ 三者皆可，开源模型唯一选择**

**接口简洁度**: `Provider` 接口仅一个方法 `Stream(ctx, Request) → <-chan Chunk`

### 2.4 Hook 系统 — ★★★★☆

#### 10 个事件挂钩点

```
PreToolUse       → 工具执行前（✅ 可阻断）
PostToolUse      → 工具执行后
UserPromptSubmit → 轮次开始前（✅ 可阻断）
Stop             → 轮次结束后
PostLLMCall      → 模型流结束后（✅ 可改写推理内容）
SessionStart     → 会话开始
SessionEnd       → 会话关闭
SubagentStop     → 子代理结束
Notification     → 需要用户注意
PreCompact       → 压缩开始前（✅ 可注入压缩指导）
```

#### 执行机制
- Hook 是 **shell 命令**，JSON 载荷从 stdin 传入
- 退出码: 0=通过, 2=阻断, 其他=警告
- 项目级 + 用户级 settings.json 配置

#### 缺口

| 缺失能力 | 影响 |
|:---------|:-----|
| ❌ **HTTP 钩子** | 不能通过 Webhook 通知外部系统 |
| ❌ **异步钩子** | 所有钩子同步执行，延长轮次响应时间 |
| ❌ **MCP 工具钩子** | 不能钩住 MCP 插件的工具调用 |

### 2.5 Skill 技能系统 — ★★★★☆

#### 能力矩阵

| 维度 | 支持度 |
|:-----|:------|
| 发现路径 | `.reasonix / .agents / .agent / .claude` 4 种 |
| 作用域 | project > custom > global > builtin |
| 目录布局 | `name/SKILL.md` 或 `name.md` |
| 加载方式 | 仅索引 (name+description) 入缓存前缀，body 按需加载 |
| 运行模式 | inline（同上下文） / subagent（隔离上下文） |
| 子代理限制 | `allowed-tools` 白名单过滤、`model` 模型覆盖 |
| 跨工具兼容 | Claude Code 的 `.claude/skills/` 可直接迁移 |

#### 缺口
- ❌ 无运行时热修改的命令行 UI
- ⚠️ 不能禁用单个内置 skill（只能全关）

### 2.6 MCP / 插件系统 — ★★★★☆

#### 传输层

| 传输 | 状态 | 用途 |
|:-----|:----|:-----|
| **stdio** | ✅ 完整 | 本地插件子进程 |
| **Streamable HTTP** | ✅ 完整 | 远程 MCP 服务器 |
| **Legacy SSE** | ✅ 完整 | 兼容旧协议 |

#### 启动层级

```
eager      → 启动时阻塞，必须成功    （核心 MCP）
lazy       → 首次调用才连接          （按需 MCP）
background → 异步启动，不阻塞主流程  （非关键 MCP）
```

自动降级: 慢速 eager 服务器自动降为 lazy。

#### 缺口

| 缺失能力 | 影响 |
|:---------|:-----|
| ❌ **MCP 执行超时** | 插件工具可能无限挂起 |
| ❌ **自动重连** | 断开后需手动重连 |
| ❌ **插件市场** | 无发现/安装/更新机制 |
| ❌ **ACP 模式下 MCP 受限** | 仅支持 stdio，不支持 HTTP/SSE |

### 2.7 代码智能 — ★★★★☆

#### LSP 工具（4 个）

| 工具 | 描述 | 状态 |
|:-----|:-----|:----|
| `lsp_definition` | 跳转到定义 | ✅ |
| `lsp_references` | 列出引用 | ✅ |
| `lsp_hover` | 类型签名 + 文档 | ✅ |
| `lsp_diagnostics` | 错误/警告（2s 超时） | ✅ |

#### CodeGraph 工具（7 个）

| 工具 | 复杂度 | 描述 |
|:-----|:------|:-----|
| `codegraph_context` | ★★★ 综合 | **首选**：入口+符号+代码 |
| `codegraph_search` | ★ | 按名称查符号 |
| `codegraph_callers` | ★ | 谁调了此函数 |
| `codegraph_callees` | ★ | 此函数调了谁 |
| `codegraph_impact` | ★★ | 修改影响分析 |
| `codegraph_trace` | ★★ | A→B 完整调用路径 |
| `codegraph_files` | ★ | 项目文件树+符号计数 |

#### 缺口

| 缺失能力 | 影响 |
|:---------|:-----|
| ❌ **lsp_completion** | 不能请求代码补全建议 |
| ❌ **代码格式化** | 无工具触发 formatter |
| ⚠️ **diagnostics 超时短** | 大型项目 2s 可能不够 |
| ⚠️ **CodeGraph Go-only** | 对 JS/Python 项目，树搜索器符号分析可能不完整 |

### 2.8 安全与权限 — ★★★★☆

#### 四层防护架构

```
Layer 1: OS 沙箱 (macOS Seatbelt)    → 文件系统 + 网络隔离
Layer 2: confine                      → bash 限制在 workspace 内
Layer 3: 权限策略                     → deny → allow → ask 规则链
Layer 4: 交互式审批                   → 用户确认每次工具调用
```

#### 权限模式

| 模式 | 说明 |
|:-----|:------|
| deny 规则 | 绝对禁止（如 `Bash(rm *)`）|
| allow 规则 | 直接放行（如 `Read(./src/**)`）|
| ask 规则 | 需要用户确认 |
| YOLO 模式 | 跳过审批（deny 仍然生效）|
| 持久化规则 | "Always allow" 写入 config |

#### 缺口

| 缺失能力 | 严重度 | 说明 |
|:---------|:------|:-----|
| ⚠️ Linux 无 Seatbelt | 🟡 中 | 仅 confine+policy 两级 |
| ⚠️ Windows 基本无沙箱 | 🟡 中 | 仅有 confine |
| ❌ Docker 沙箱 | 🟢 低 | Claude Code 支持 Docker + dev container |

### 2.9 证据与回环防护 — ★★★★★（独家）

这是 Reasonix 最独特的创新点，Claude Code 和 Codex 均无对应物。

#### 证据系统流程

```
每轮工具调用
    ↓
Ledger 记录 (工具名 + 参数 + 成功/失败 + 输出)
    ↓
模型的 complete_step 声明 "我做了 X"
    ↓
系统交叉验证:
  ├─ 检查 Ledger 中是否有成功的 X 操作
  ├─ 检查命令是否真的执行了
  ├─ 检查文件是否真的写入了
  └─ 检查是否有未签字的 todo
    ↓
通过 → 继续 | 不通过 → 阻断最终答案
```

#### 回环双防护

| 机制 | 检测 | 触发 | 动作 |
|:-----|:-----|:-----|:-----|
| Storm Breaker | (tool, error) 签名 | 3 次相同 | 注入中断消息 |
| Repeat-Success Blocker | (tool, args) 签名 | 3 次相同写入 | 直接阻断 |

### 2.10 记忆系统 — ★★★★☆

| 记忆类型 | 作用域 | 持久化 |
|:---------|:------|:-------|
| REASONIX.md | 项目/本地/用户 | 手动编辑 |
| 自动记忆 (4 类) | user/feedback/project/reference | 自动保存 |
| MEMORY.md 索引 | 全部自动记忆 | 自动更新 |

**关键设计**: 记忆变更走 turn-tail 注入，不修改缓存稳定前缀。

### 2.11 上下文压缩 — ★★★★★

| 特性 | 详情 |
|:-----|:------|
| 阈值策略 | soft(0.5)→通知, compact(0.8)→触发, force(0.9)→强制 |
| 尾部保留 | Token-budgeted（不是消息数） |
| stuck 锁 | 超窗口时停止压缩 |
| tok_per_char | 来自实际 provider 用量数据 |
| 外部指导 | PreCompact hook 可注入压缩提示 |

### 2.12 会话管理 — ★★★★★

| 能力 | 实现 |
|:-----|:------|
| 持久化 | JSONL 原子写入（tmp + rename）|
| 自动保存 | 每次轮次后 `snapshotActivityIfChanged` |
| 恢复 | `--continue`（最新）/ `--resume <path>`（指定）|
| 分支 | ForkNamed + SwitchBranch + BranchTreeText |
| 检查点 | Rewind（代码/对话/二者）|
| 跨进程 | ACP session/load |

### 2.13 国际化 (i18n) — ★★★★☆

- 完整的 Messages 结构，编译时检查缺失翻译
- 当前支持中英文
- 范围适配: CLI 交互文本（非系统提示）

### 2.14 成本控制 — ★★★★☆

- `internal/billing/balance.go` — 余额查询（DeepSeek 格式）
- `internal/provider/pricing.go` — token 计价
- `Usage` 统计: prompt tokens, completion tokens, cache hit/miss, reasoning tokens
- `-metrics` 参数: 输出 JSON 用量报告

### 2.15 前端覆盖 — ★★★☆☆

| 前端 | 状态 | 评分 |
|:-----|:-----|:----|
| CLI TUI (Bubbletea) | ✅ 正常缓冲渲染、历史 scrollback | ★★★★★ |
| HTTP/SSE Web | ✅ 23 端点、内嵌 Web UI | ★★★★ |
| Wails 桌面 | ✅ 多 Tab、MCP/记忆管理 | ★★★ |
| ACP 协议 | ✅ JSON-RPC 2.0、编辑器集成 | ★★★★ |
| **VS Code 插件** | ❌ | ⭐ 最大缺失 |
| **JetBrains 插件** | ❌ | ⭐ |

---

## 第三章：缺口分级与影响

### 🔴 高影响缺口（影响核心体验）

| 缺口 | 影响 | 补救成本 |
|:-----|:-----|:--------|
| **无 VS Code/JetBrains 插件** | 开发者主要工作环境无法接入 | 🔴 高（需开发/维护 IDE 插件）|
| **无 Git 原生工具** | git 操作不可见、不可控、不可审批 | 🟡 中（封装 5 个结构化工具）|
| **无多模态/视觉** | 不能处理 UI 截图、流程图、设计稿 | 🔴 高（需模型层支持）|

### 🟡 中影响缺口（影响效率）

| 缺口 | 影响 | 补救成本 |
|:-----|:-----|:--------|
| 无正则搜索替换 | 跨文件重构效率低 | 🟢 低（新增一个 replaceAll 工具）|
| Linux 沙箱缺失 | Linux 用户安全防护降级 | 🔴 高（需要 Bubblewrap 或 Docker 沙箱）|
| MCP 无超时/重连 | 插件可靠性不足 | 🟡 中 |
| HTTP 钩子缺失 | 不能对接外部事件系统 | 🟡 中 |

### 🟢 低影响缺口（锦上添花）

| 缺口 | 补救成本 |
|:-----|:--------|
| 批量文件操作 | 🟢 低 |
| 代码补全工具 | 🟡 中 |
| 代码格式化工具 | 🟢 低 |
| 异步钩子 | 🟡 中 |
| 热加载单个 skill 禁用 | 🟢 低 |

---

## 第四章：代码质量评估

### 架构亮点

| 特征 | 评价 |
|:-----|:-----|
| 包职责清晰 | ★★★★★ — 29 包各司其职 |
| 接口隔离 | ★★★★★ — Provider(1方法)、Tool(5方法)、Sink(1方法) |
| 编译隔离 | ★★★★★ — CLI(`CGO_ENABLED=0`) 与 Desktop(Wails/CGO) 分模块 |
| 测试覆盖 | ★★★★☆ — 部分包有 e2e 测试，整体较好 |
| 事务安全 | ★★★★★ — JSONL 原子写入、原子 multi_edit、session 读写锁 |

### 重构候选

| 文件 | 行数 | 问题 | 建议 |
|:-----|:----|:-----|:-----|
| `control/controller.go` | ~1900 | 管理 session+MCP+记忆+审批+checkpoint+命令解析 | 拆分出 Commands、Checkpointer、Approver 子对象 |
| `agent/agent.go` | 1239 | Run() 混合流接收+并行调度+风暴检测+证据记录 | 抽出 StreamConsumer、GuardDetector 等 |
| `cli/cli.go` | 1644 | 模型探测+配置向导+子命令处理混杂 | 分离 wizard、子命令 handler |
| `serve/serve.go` | 964 | 所有 handler 写在一个文件 | 按职责拆分路由文件 |
| `acp/service.go` | 972 | session 管理样板多 | 可复用 controller 的 session 逻辑 |

---

## 第五章：综合改进路线图

### 短期可补（低代码成本）

```
1.  └── Git 工具（5个）       → ~200 行，新增 internal/tool/git/
      ├── git_status
      ├── git_diff
      ├── git_commit
      ├── git_branch
      └── git_log

2.  正则搜索替换              → ~80 行，扩展 grep 或新增 replace_tool

3.  CLI run 会话持久化        → ~20 行，加 --continue/--resume 和 SetSessionPath

4.  批量文件操作              → ~60 行，mv/rm/cp 安全封装
```

### 中期可补（中等代码成本）

```
5.  HTTP 钩子                  → 复用现有 Hook 框架，加 HTTP 传输
6.  MCP 超时/重连              → plugin/client.go 加超时控制
7.  LSP 代码补全               → lsp/tool.go 加 completion 工具
8.  CodeGraph 多语言扩展       → 扩展树搜索器语言支持
```

### 长期（高代码成本）

```
9.  VS Code 插件              → 通过 ACP 或 HTTP API 对接
10. Linux 沙箱 (Bubblewrap)   → 类似 macOS Seatbelt 实现
11. 多模态支持                → 需 provider 层 + ACP 协议 + 工具链
```

---

## 第六章：总结

### Reasonix 的独特价值

```
独家能力（Claude Code / Codex 没有）:
├── 多 LLM 提供商可插拔 — 开源模型唯一选择
├── 证据系统 — complete_step 防幻觉
├── 缓存稳定前缀 — 架构级设计，非 API 端缓存
├── 回环双防护 — Storm + Repeat 双保险
├── CodeGraph 确定性符号分析
├── MCP 启动层级 + 自动降级
├── 前端共享 Controller — TUI/HTTP/Desktop 行为一致
└── Go 库可直接引用
```

### Reasonix 的能力短板

```
与行业标杆的差距:
├── 无 IDE 插件（🔴 最大缺失）
├── 无 Git 原生工具（🔴 次大缺失）
├── 无多模态支持（🟡 中）
├── 无 Linux 沙箱（🟡 中）
├── 工具覆盖面不够广（17个 vs 应有 ~25个）
└── 核心文件过大（重构候选）
```

### 最终评分

| 维度 | 评分 | 趋势 |
|:-----|:----:|:-----|
| **架构设计** | ★★★★★ | 稳固 |
| **核心功能** | ★★★★☆ | 小幅提升中 |
| **工具覆盖面** | ★★★☆☆ | 需系统补全 |
| **生态系统** | ★★☆☆☆ | 需大幅建设 |
| **代码质量** | ★★★★☆ | 需重构核心文件 |
| **综合** | ★★★★☆ | **成熟，可投入生产** |
