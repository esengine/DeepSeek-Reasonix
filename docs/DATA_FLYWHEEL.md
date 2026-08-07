# Data Flywheel — 数据飞轮架构（通用，以 Reasonix 为案例）

> 定位：不是只优化某一个软件，而是把 **agent 工作流产生的数据**（会话、工具调用、验证结果、
> 评测轨迹）建成一个"采集 → 清洗 → 复用 → 验证"的闭环飞轮。本设计通用（可复用于任何
> agent / 工具集 / 项目），落地以 Reasonix（Go agent + 本机 MCP 工具集）为第一个案例。

## 1. 四环架构

```
        ┌──────────────────────────────────────────────────────────┐
        │                   ③ 复 用（数据反哺）                      │
        │  · 记忆目录 NOTES.md + compaction 摘要（context eng.）      │
        │  · 成功轨迹 → few-shot 示例（CoSPlay 式蒸馏）               │
        │  · 失败轨迹 → 记忆库（避免重犯）                           │
        └───────────────▲───────────────────────────▲──────────────┘
                        │                           │
        ┌───────────────┴──────────┐   ┌────────────┴───────────────┐
        │  ② 清 洗（数据变资产）    │   │  ④ 验 证（数据验证进步）     │
        │  · 轨迹结构化 JSONL        │   │  · 评测接入（goaleval /      │
        │  · LLM-as-judge 质量标签   │   │    Terminal-Bench 子集）     │
        │  · BM25 检索索引           │   │  · 失败轨迹回流 ③            │
        └───────────────▲──────────┘   └────────────▲───────────────┘
                        │                           │
        ┌───────────────┴───────────────────────────┴───────────────┐
        │  ① 采 集（数据从哪来）                                      │
        │  · agent 事件流（对齐 OTel GenAI semconv）                  │
        │  · MCP 调用记录（gen_ai.tool.* / gen_ai.mcp.*）             │
        │  · 会话 .events.jsonl / compaction archive / 验证结果       │
        └──────────────────────────────────────────────────────────┘
```

**核心不变量**：
1. 采集层不丢字段（append-only JSONL，统一 schema 起步）。
2. 复用层不读原始会话——只读清洗后的结构化资产（轨迹/记忆/标签）。
3. 验证层的结果必须回流（失败轨迹 → 记忆库 = 飞轮转起来的标志）。

## 2. 数据 Schema 草案（JSONL 字段表）

### 2.1 Agent 事件流（`<session>.events.jsonl`，对齐 OTel `gen_ai.agent.*`）

| 字段 | 类型 | 说明 | OTel 对应 |
|---|---|---|---|
| `ts` | string | RFC3339 时间戳 | — |
| `session` | string | 会话 id | `gen_ai.agent.id` |
| `kind` | string | 事件类型：`tool_call` / `tool_result` / `assistant_msg` / `user_msg` / `subagent` / `verify` / `compact` | `gen_ai.events` |
| `tool` | string | 工具名（tool_call/result 时） | `gen_ai.tool.name` |
| `tool_input` | string | 输入摘要（截断 ≤512B） | `gen_ai.tool.input` |
| `tool_output` | string | 输出摘要（截断 ≤1024B） | `gen_ai.tool.output` |
| `dur_ms` | int | 调用耗时 ms | `gen_ai.tool.duration` |
| `err` | string? | 错误码/错误摘要（空 = 成功） | `gen_ai.tool.error` |
| `model` | string? | 模型名（assistant_msg 时） | `gen_ai.agent.model` |
| `meta` | object? | 附加（如验证结果 verdict、subagent id） | 扩展字段 |

### 2.2 MCP 调用记录（`<data>/mcp-calls.jsonl`）

| 字段 | 类型 | 说明 |
|---|---|---|
| `ts` / `server` / `tool` | string | 时间 / MCP server / 工具名 |
| `args` | string | 参数摘要 ≤512B |
| `dur_ms` | int | 耗时 |
| `ok` | bool | 成功与否 |
| `err` | string? | 错误信息摘要 |

### 2.3 轨迹（`<data>/trajectories/<id>.jsonl`——任务级资产）

```json
{"id":"traj_01","task":"摘要","session":"s_1","ts":"...","steps":[{"kind":"tool_call","tool":"read_file","dur_ms":12},...],"verify":{"kind":"go_test","ok":true,"detail":"107 packages ok"},"judge":{"score":0.8,"label":"good","reason":"tests green, no regressions"}}
```

字段：`id` / `task` / `session` / `ts` / `steps[]`（工具调用序列摘要）/ `verify`（验证结果）/ `judge`（质量标签，见 2.4）/ `repo`（可选项目标识）。

### 2.4 质量标签（LLM-as-judge，Agent-as-a-Judge 方法）

| score | label | 判据 |
|---|---|---|
| 0.9–1.0 | excellent | 验证全绿 + 无回归 + 结构合理 |
| 0.7–0.89 | good | 验证全绿，有小瑕疵 |
| 0.5–0.69 | partial | 部分验证通过，需人工看 |
| <0.5 | failed | 验证失败 / 任务未完成（轨迹进记忆库） |

### 2.5 记忆条目（`<workspace>/.reasonix/memory/`）

- `NOTES.md`：项目级持久笔记（关键决策、约定、教训），agent 每轮可读。
- `compaction/<session>.md`：会话 compaction 摘要（Anthropic context engineering 模式）。
- `failures/<traj_id>.md`：失败轨迹教训（few-shot 复用候选）。
- 检索：`internal/retrieval` BM25 索引（新增对 memory 目录的检索入口）。

## 3. 技术雷达摘要（调研 2025-2026，落地优先级）

### P0（本轮落地）
| 技术 | 定位 | 落地方式 |
|---|---|---|
| OTel GenAI semconv | agent/tool 可观测性字段标准 | 2.1/2.2 字段对齐 |
| Context engineering（Anthropic 2025-09） | NOTES.md + compaction 摘要 | 2.5 记忆目录 |
| CoSPlay 式蒸馏 | 成功轨迹 → few-shot | 3.3 蒸馏钩子 |
| Agent-as-a-Judge | 轨迹自动打分 | 2.4 质量标签 |

### P1（后续）
| 技术 | 定位 | URL |
|---|---|---|
| Terminal-Bench 2.0 | 终端执行型评测（Docker） | https://github.com/harbor-framework/terminal-bench |
| MCP-Bench | MCP 工具使用评测 | arXiv:2508.11210 |
| A-Mem | Zettelkasten 动态记忆 | https://github.com/WujiangXu/A-mem-sys |
| Langfuse / LangSmith | 托管追踪/回放 | https://langfuse.com |
| OpenHands-10K | 轨迹公开数据集（event stream 模型参照） | https://huggingface.co/datasets/OpenHands/OpenHands-10K |
| OSWorld 2.0 | 真实桌面评测（轨迹格式参照） | https://osworld-v2.xlang.ai/ |

### P2（观望）
| 技术 | 定位 | 备注 |
|---|---|---|
| SWE-bench Verified 2.0 | 2025-11 新基准 | 细节未核实 |
| MCP Registry | MCP server 发布标准 | https://github.com/modelcontextprotocol/registry |
| mem0 / Zep / Letta | 托管记忆层 | 与本地轻量方案二选一 |
| BFCL / Gorilla | 工具调用数据标准 | 字段规范参照 |

## 4. 与现有资产的关系（复用而非重建）

| 现有资产 | 位置 | 飞轮角色 |
|---|---|---|
| 会话 events 日志 | `store/session.go`（`<name>.events.jsonl`） | 采集层事件源 |
| compaction 归档 | `agent.go` `archiveDir`（`archiveMessages`） | 采集层（compact 时保留原始消息） |
| BM25 检索 | `internal/retrieval` | 复用层检索 |
| 评测器 | `internal/goaleval` | 验证层（任务级评测） |
| 工具调用事件 | agent 循环 ToolDispatch/ToolResult | 采集层（补 gen_ai 字段） |

## 5. 落地路线（本轮）

1. **Phase 2 采集**：agent 事件补 `gen_ai` 字段（tool/dur_ms/err 从 ToolDispatch/ToolResult 事件扩展）；MCP 调用记录层（`internal/plugin` 调用路径记 JSONL）。
2. **Phase 3 清洗复用**：轨迹结构化（任务完成时聚合会话事件 → `trajectories/<id>.jsonl`）；记忆目录（NOTES.md + compaction 摘要写入 + BM25 检索入口）；LLM-as-judge 打分钩子（`internal/flywheel` 包，judge 接口可注入）。
3. **Phase 4 验证**：goaleval 复用跑任务集，失败轨迹自动进 `memory/failures/`；全仓回归 + 提交。

## 6. 风险与边界

- **不可信数据**：事件/记忆文本进 prompt 时按不可信框架包裹（复用 watch 注入模式）。
- **体积控制**：tool_input/output 截断；compaction 摘要限长（≤8KB）；记忆目录按项目独立。
- **不引外部依赖**：本轮零新依赖（JSONL + 标准库 + 已有 BM25），Langfuse/向量库等为 P1 可选。
- **飞轮有效性**：判据 = 失败轨迹回流数 > 0 且复用后同类失败率下降（人工抽查）。
