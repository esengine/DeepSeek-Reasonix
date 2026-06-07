# Reasonix 智能体能力评估报告

**报告日期**: 2026-06-07  
**评估范围**: 从智能体（Agent）能力角度评估项目成熟度，不涉及商业/插件市场层面  
**评估方法**: 源代码分析（~19,000 行 Go 代码）

---

## 总评

| 维度 | 评分 | 一句话概括 |
|:-----|:----:|:----------|
| **工具系统 (Tools)** | ★★★★★ | 17 个内置工具 + 动态注册，完备且成熟 |
| **LLM 提供商** | ★★★★★ | 可插拔架构，开源模型中独一档 |
| **提示缓存** | ★★★★★ | 缓存稳定前缀是架构级设计 |
| **回环防护** | ★★★★★ | 风暴检测 + 重复阻断，双保险 |
| **证据系统** | ★★★★★ | 防幻觉的独特设计 |
| **MCP/插件** | ★★★★☆ | 协议完整，但缺插件发现/市场 |
| **Skill 技能** | ★★★★☆ | 多种约定兼容，子代理隔离 |
| **Hook 钩子** | ★★★★☆ | 10 个事件点，但缺 HTTP hooks |
| **LSP/CodeGraph** | ★★★★☆ | 14 语言 LSP + 树搜索器符号分析 |
| **权限/沙箱** | ★★★★☆ | 四层防护，但 Linux 沙箱不完整 |
| **记忆系统** | ★★★★☆ | 层级文档 + 自动记忆 |
| **前端覆盖** | ★★★☆☆ | TUI+HTTP+Desktop，缺 IDE 插件 |
| **会话管理** | ★★★★★ | 持久化 + 恢复 + 分支 + 检查点 |
| **代码质量** | ★★★★☆ | 架构清晰，但核心文件过大 |

**综合评分: ★★★★☆ (成熟)**

---

## 一、工具系统 (Tools) — ★★★★★

### 内置工具清单（17 个）

| 工具 | 只读 | 类别 | 特点 |
|:-----|:----:|:----|:-----|
| `bash` | ❌ | 执行 | 登录 shell PATH 探测缓存，Seatbelt 沙箱，120s 超时 |
| `read_file` | ✅ | 读取 | 支持 offset/limit 分页，UTF-16 兼容，行号前缀 |
| `write_file` | ❌ | 写入 | 自动建父目录，原子写入（tmp+rename） |
| `edit_file` | ❌ | 写入 | 精确字符串替换，唯一性校验 |
| `multi_edit` | ❌ | 写入 | 批量原子编辑，失败不回滚整个文件 |
| `delete_range` | ❌ | 写入 | 双锚点范围删除，非唯一锚点报错 |
| `delete_symbol` | ❌ | 写入 | AST 驱动删除 Go 符号，保留注释变空白 |
| `ls` | ✅ | 读取 | 递归/非递归，文件大小显示 |
| `glob` | ✅ | 读取 | **递归 `**/*` 匹配** |
| `grep` | ✅ | 搜索 | ripgrep 后端，路径定位输出 |
| `web_fetch` | ✅ | 获取 | HTML→文本净化，SSRF 防护 |
| `notebook_edit` | ❌ | 写入 | Jupyter notebook 单元编辑 |
| `todo_write` | ❌ | 元 | 结构化任务列表 |
| `complete_step` | ❌ | 元 | 证据交叉验证签字 |
| `bash_output` | ✅ | 后台 | 读取后台 bash 输出 |
| `wait_job` | ✅ | 后台 | 等待后台任务 |
| `kill_shell` | ❌ | 后台 | 终止后台任务 |

### 工具系统成熟度标志

- ✅ 静态 `ReadOnly()` 标记 → 并行执行只读工具
- ✅ `Previewer` 接口 → 写入前预览 diff，支撑 checkpoint 和审批
- ✅ `confine` 机制 → bash 限制在 workspace 内
- ✅ 所有工具通过 `init()` 自注册，无需手动清单
- ✅ Schema 规范化 + 缓存

### 不足

- 缺少 `timeout` 参数（只有 bash 有硬编码 120s 超时）
- 没有工具间的依赖/条件执行机制

---

## 二、LLM 提供商 (Provider) — ★★★★★

### 支持的提供商

| 后端 | 类型 | 状态 |
|:-----|:----|:----|
| OpenAI 兼容 | `openai` | ✅ 完整实现，含 think token 处理 |
| Anthropic | `anthropic` | ✅ 完整实现，含推理签名 |
| 自定义 | — | ✅ `Factory` 接口可注册任意后端 |

### 架构亮点

```
provider.Register("openai", New)    // internal/provider/openai
provider.Register("anthropic", New)  // internal/provider/anthropic
```

- `Provider` 接口仅一个方法：`Stream(ctx, Request) → <-chan Chunk`
- `SanitizeToolPairing` — 修复历史以符合各 API 合约
- 缓存命中/未命中 token 标准化（DeepSeek ↔ OpenAI 两种 Shape）
- Pricing 模型分离

### 与竞品对比

| 能力 | Reasonix | Claude Code | Codex |
|:-----|:--------:|:-----------:|:-----:|
| 多提供商 | ✅ | ❌ 仅 Anthropic | ❌ 仅 OpenAI |
| 开源可用 | ✅ | ❌ | ✅（仅OpenAI） |
| DeepSeek 支持 | ✅ | ❌ | ❌ |

---

## 三、Hook 系统 — ★★★★☆

### 事件点（10 个）

| 事件 | 时机 | 可阻断 | 典型用途 |
|:-----|:----|:------|:--------|
| `PreToolUse` | 工具执行前 | ✅ 阻断 | 安全检查、日志 |
| `PostToolUse` | 工具执行后 | ❌ | 审计、告警 |
| `UserPromptSubmit` | 轮次开始前 | ✅ 阻断 | 内容过滤 |
| `Stop` | 轮次结束后 | ❌ | 清理、通知 |
| `PostLLMCall` | 模型流结束后 | ❌ | **推理内容替换** |
| `SessionStart` | 会话开始 | ❌ | 环境初始化 |
| `SessionEnd` | 会话关闭 | ❌ | 清理资源 |
| `SubagentStop` | 子代理结束 | ❌ | 结果汇总 |
| `Notification` | 需要用户注意 | ❌ | 桌面通知 |
| `PreCompact` | 压缩开始前 | ❌ | **注入压缩指导** |

### 钩子执行机制

- Hook 是 shell 命令（非 HTTP），JSON 载荷从 stdin 传入
- 退出码 0=通过，2=阻断，其他=警告
- cwd 固定为项目根目录
- Runner 可空（nil *Runner 是无操作）

### 亮点

- **PostLLMCall** 可替换推理内容——不仅是读，还能改写模型的思维过程
- **PreCompact** 可注入压缩指导——让外部脚本影响上下文压缩策略
- 支持 Hooks 配置在 settings.json 中，项目级 + 用户级

### 不足

- ❌ 不支持 **HTTP 钩子**（代码中有 `allowedHttpHookUrls` 配置项但注释说已保留给将来）
- ❌ 不支持异步钩子（所有钩子同步执行，延长轮次时间）
- ⚠️ 测试覆盖有限（runner_test.go 主要测试文件，但覆盖不全）

---

## 四、Skill 技能系统 — ★★★★☆

### 能力概要

| 维度 | 支持度 |
|:-----|:------|
| 发现路径 | `.reasonix/skills/` , `.agents/skills/` , `.agent/skills/` , `.claude/skills/` |
| 作用域 | project > custom > global > builtin |
| 目录布局 | `name/SKILL.md` 或 `name.md` |
| 热加载 | ✅ `/ <name>` 按需加载 |
| 子代理隔离 | ✅ `runAs: subagent` → 独立上下文 |
| 子代理工具限制 | ✅ `allowed-tools:` 前置过滤 |
| 跨工具兼容 | ✅ Claude Code 的 `.claude/skills/` 可直接迁移 |
| 与 ACP 集成 | ✅ 子代理模式通过 ACP session 运行 |

### 索引机制

```
Index（缓存稳定前缀）
  └─ 仅 name + description 进入 system prompt
      └─ body 按需加载 → 不增加前缀 token
```

### 不足

- ❌ 没有运行时热修改能力的 UI/命令
- ⚠️ 不能禁用单个内置 skill（只能 `DisableBuiltins: true` 全关）
- ⚠️ 子代理父上下文不能传递文件状态变化

---

## 五、MCP / 插件系统 — ★★★★☆

### 传输层支持

| 传输 | 状态 | 详情 |
|:-----|:----|:-----|
| stdio | ✅ 完整 | 子进程 + JSON-RPC 2.0 |
| Streamable HTTP | ✅ 完整 | 无状态 HTTP 流 |
| Legacy SSE | ✅ 完整 | HTTP + SSE 事件流 |

### 启动层级

```
eager    → 启动时阻塞，必须成功
lazy     → 首次调用 tool 时才连接
background → 异步启动，不阻塞主流程
```

**自动降级**: 某 MCP 服务器如果多次启动慢，自动从 eager 降为 lazy。

### 热管理

| 操作 | ACP | HTTP/SSE | CLI |
|:-----|:---:|:--------:|:---:|
| 添加 MCP 服务器 | ❌ | ✅ | ✅ |
| 移除 MCP 服务器 | ❌ | ✅ | ✅ |
| 查看 MCP 状态 | ❌ | ✅ | ✅ |
| 列出 MCP 工具 | ❌ | ✅ | ✅ |

### 命名与去重

- `mcp__<server>__<tool>` 命名空间防止冲突
- `StripRawPrefix` 避免冗余前缀（如 `codegraph_context` → `context`）
- FNV hash 后缀规范化名称

### 不足

- ❌ 无 MCP 工具执行超时控制
- ❌ 无 MCP 服务器健康检查/自动重连
- ❌ 无插件市场
- ⚠️ ACP 模式不支持声明 MCP 服务器（HTTP: false, SSE: false）

---

## 六、代码智能 (LSP + CodeGraph) — ★★★★☆

### LSP 支持（14 种语言）

```
gopls, rust-analyzer, typescript-language-server,
pyright, clangd, ... → 通过 PATH 解析，不捆绑
```

| 工具 | 描述 |
|:-----|:-----|
| `lsp_definition` | 跳转到定义 |
| `lsp_references` | 列出所有引用 |
| `lsp_hover` | 类型签名 + 文档 |
| `lsp_diagnostics` | 诊断信息（2s 超时等待） |

### CodeGraph（树搜索器 + SQLite FTS5）

| 工具 | 复杂度 | 描述 |
|:-----|:------|:-----|
| `codegraph_context` | 综合性 | **首选**：入口点 + 相关符号 + 代码 |
| `codegraph_search` | 快速 | 按名称搜索符号 |
| `codegraph_callers` | 快速 | 谁调用了这个函数 |
| `codegraph_callees` | 快速 | 这个函数调用了谁 |
| `codegraph_impact` | 深度 | 修改此符号会影响到谁 |
| `codegraph_trace` | 路径 | 从符号 A 到符号 B 的完整路径 |
| `codegraph_files` | 快速 | 项目文件树 |

**亮点**: CodeGraph 是独立 MCP 服务器（不在模型内部），提供确定性分析。按需下载（不捆绑）。项目级别并行索引，热项目自动提升为 eager 启动。

### 不足

- ⚠️ CodeGraph 索引需时间（大型项目首次约数秒）
- ❌ 没有代码补全工具（lsp_completion）
- ⚠️ diagnostics 只等待 2s，大型项目可能不够

---

## 七、权限与安全 — ★★★★☆

### 四层防护

```
① OS 沙箱 (macOS Seatbelt) → 文件系统 + 网络隔离
② confine 机制              → bash 限制在 workspace 内
③ 权限策略                  → deny/allow/ask 规则
④ 交互式审批                → 用户确认每次工具调用
```

### 权限模式

| 模式 | 说明 |
|:-----|:------|
| deny → allow → ask | 优先级递减的规则链 |
| YOLO/Bypass | 跳过审批（deny 仍然生效）|
| 交互式审批 | 前端收到 ApprovalRequest → 用户选择 |
| 持久化规则 | "Always allow" 写入 config |

### 不足

- ⚠️ Linux 没有 Seatbelt（仅 macOS），使用 confine + policy 两级
- ⚠️ Windows 基本无沙箱
- ⚠️ 没有计划运行时的沙箱降级策略

---

## 八、证据与回环防护 — ★★★★★ (独特)

### 证据系统 (Evidence)

```
每轮工具调用 → Ledger 记录
complete_step → 交叉验证模型声明 vs 实际工具输出
                ├─ HasSuccessfulCommand  → bash 确实执行了
                ├─ HasSuccessfulWrite    → 文件确实写入了
                ├─ MatchLatestTodoStep   → todo 状态匹配
                └─ UnverifiedCompletedTodos → 发现未签字的步骤
```

**这是 Reasonix 最独特的设计**，Claude Code 和 Codex 均无对应物。

### 回环双防护

| 机制 | 检测对象 | 触发条件 | 动作 |
|:-----|:--------|:--------|:-----|
| Storm Breaker | (tool, error) 签名 | 3 次相同错误 | 注入中断消息 |
| Repeat-Success Blocker | (tool, arguments) 签名 | 3 次相同成功写入 | 直接阻断 |

---

## 九、记忆系统 — ★★★★☆

### 结构

| 类型 | 范围 | 作用 |
|:-----|:-----|:-----|
| 文档记忆 | REASONIX.md / AGENTS.md / CLAUDE.md | 项目级/本地/用户级层级 |
| 自动记忆 | 前端数据文件（per-project） | 分为 user/feedback/project/reference 四种 |
| MEMORY.md 索引 | 全部自动记忆的目录索引 | 快速查找 |

### 记忆修改

```
修改请求 → memory.Queue（不修改缓存稳定前缀）
       ↓
Turn-tail 注入 → 当前轮次立即生效
       ↓
下一会话 → 重建前缀 → 变更持久化
```

### 工具

| 工具 | 作用 |
|:-----|:-----|
| `remember` | 保存持久事实到项目记忆 |
| `forget` | 删除记忆事实 |
| `#<note>` | 快速追加到 REASONIX.md（quickadd） |

---

## 十、上下文压缩 — ★★★★☆

### 三阈值策略

| 阈值 | 比率 | 行为 |
|:-----|:----|:-----|
| softCompactRatio | 0.5 | 通知但不压缩 |
| compactRatio | 0.8 | 触发压缩 |
| compactForceRatio | 0.9 | 强制压缩低价值折叠 |

### 亮点

- **Token-budgeted 保留尾部**（不是消息计数）——防止大型工具输出导致每轮压缩
- **compactStuck 锁**：系统提示 + 一轮对话已超窗口时停止压缩
- **tok_per_char** 来自实际提供商使用数据，非硬编码
- **PreCompact hook** 可注入外部压缩指导

---

## 十一、会话管理 — ★★★★★

| 能力 | 实现 | 状态 |
|:-----|:-----|:----|
| JSONL 持久化 | `Session.Save()` / `LoadSession()` | ✅ 原子写入 (tmp+rename) |
| 自动保存 | `snapshotActivityIfChanged()` | ✅ 每次轮次后自动保存 |
| 分支 | `ForkNamed()` / `SwitchBranch()` | ✅ 基于检查点的分支树 |
| 检查点 | `Checkpoints()` / `Rewind()` | ✅ 可回退代码/对话/二者 |
| 恢复 | `--continue` / `--resume` | ✅ chat + serve 模式 |
| 列表 | `ListSessions()` | ✅ 按最近活跃排序 |
| 跨进程恢复 | ACP `session/load` | ✅ |

---

## 十二、前端覆盖 — ★★★☆☆

| 前端 | 状态 | 成熟度 |
|:-----|:-----|:------|
| CLI TUI (Bubbletea) | ✅ 完整 | ★★★★★ — 正常缓冲渲染、历史 scrollback、任务面板 |
| HTTP/SSE Web | ✅ 完整 | ★★★★☆ — 23 个端点，内嵌 Web UI |
| Wails 桌面 | ✅ 完整 | ★★★☆☆ — 多 Tab、MCP/记忆管理，但依赖 WebView |
| ACP 协议 | ✅ 完整 | ★★★★☆ — 标准 JSON-RPC，编辑器集成 |
| **VS Code 插件** | ❌ 无 | ⭐ — **最大空白** |
| **JetBrains 插件** | ❌ 无 | ⭐ |

---

## 十三、代码质量 — ★★★★☆

### 强项

- **包结构清晰**：29 个内部包，各司其职
- **接口隔离**：`Provider`（单方法接口）、`Tool`（4方法）、`Sink`（1方法）
- **测试覆盖率**：整体较好，部分包有 e2e 测试
- **编译隔离**：CLI (`CGO_ENABLED=0`) 与 Desktop (Wails/CGO) 分两个模块

### 弱项

| 文件 | 行数 | 问题 |
|:-----|:----|:-----|
| `internal/agent/agent.go` | 1239 | `Run()` 主循环包含 storm 检测、重复阻断、并行调度等混杂逻辑 |
| `internal/control/controller.go` | ~1900 | 违反了单一职责（session、MCP、记忆、审批、hook、checkpoint） |
| `internal/serve/serve.go` | 964 | handler 方法过多，可提取子路由 |
| `internal/acp/service.go` | 972 | session 管理需大量样板代码 |
| `internal/cli/cli.go` | 1644 | setup 逻辑、模型探测、配置向导混杂 |

---

## 十四、风险评估

| 风险 | 等级 | 说明 |
|:-----|:----|:------|
| `agent.Run()` 单方法过长 | 🟡 中 | 核心循环难以单独测试，修改易引入回归 |
| 多提供商适配膨胀 | 🟡 中 | 每个新 LLM 需要 Stream() 适配 + token 计算 + SanitizeToolPairing |
| 无 IDE 插件 | 🟡 中 | 限制了主流开发者日常使用场景 |
| Desktop 前端测试不足 | 🟢 低 | desktop/frontend 没有持续集成 |
| Linux 沙箱缺失 | 🟢 低 | 只有 macOS 有 Seatbelt 沙箱 |
| 文档分散 | 🟢 低 | 关键架构需要读源码才能理解 |

---

## 十五、总结

```
Reasonix 智能体能力成熟度
═══════════════════════

已完成（★★★★★）:
  工具系统 — 17 个内置工具，ReadOnly/Previewer/confine
  LLM 提供商 — 可插拔，开源模型唯一选择
  提示缓存 — 缓存稳定前缀架构
  回环防护 — Storm + Repeat 双保险（独家）
  证据系统 — 防幻觉设计（独家）
  会话管理 — 持久化/分支/检查点/恢复

基本成熟（★★★★☆）:
  MCP — 三层级 + 自动降级，缺市场/发现
  LSP — 14 语言，缺补全
  CodeGraph — 确定性符号分析
  Hook — 10 事件点，缺 HTTP/异步
  Skill — 多约定兼容，子代理隔离
  记忆 — 层级 + 自动，缺同步机制
  权限/沙箱 — 四层防护，Linux 不全
  上下文压缩 — 三阈值策略

需加强（★★★☆☆）:
  前端覆盖 — 缺 VS Code/JetBrains 插件

开发者 100% 可控制的核心:
  ✓ LLM 选择（深层次可替换）
  ✓ 工具系统扩展
  ✓ Hook 脚本注入
  ✓ Skill 技能编写
  ✓ MCP 服务器连接
  ✓ 安全策略配置
  ✓ 记忆管理
```
