# 03 · Plan 模式 / 自动计划相关提示词

Plan 模式是 Reasonix 的"研究 → 出方案 → 用户审批 → 执行"流水线。这条线一共涉及三段提示词：

1. **`PlanModeMarker`** — 用户回合前缀，告诉模型当前是 plan-only 模式；
2. **`autoPlanClassifierPrompt`** — 一个独立小模型轮次的 system prompt，判断是否需要进入 plan；
3. **`planApprovedMessage`** — 用户审批通过后，由 controller 替模型注入的"开干"信号。

把这三段放在用户回合（而不是改 system 槽），就是为了**不抖动前缀缓存**——见 `controller.go` 注释 `// rides in the user turn, not the system prompt or tool schema, so plan toggles preserve cache shape.`

---

## 3.1 `planmode.Marker`（plan 模式块）

| 元信息 | 值 |
| --- | --- |
| **常量名** | `planmode.Marker`，在 [`internal/control/input.go`](../../internal/control/input.go) 中以 `PlanModeMarker = planmode.Marker` 暴露 |
| **来源文件** | [`internal/planmode/policy.go`](../../internal/planmode/policy.go) |
| **何时注入** | Plan 模式 ON 时，每个用户回合的最前面 `PlanModeMarker + "\n\n" + 真正用户输入`。`legacyPlanModeMarker` 用作向后兼容剥离。 |
| **作用** | 把"plan-only / 不能写文件 / 必须分层出 numbered phase + 子 bullets"这套硬约束塞给模型。 |

### 原文

```
[Plan mode — planning only. You may research the codebase and web, ask clarifying questions with ask, maintain planning state with todo_write, and delegate isolated read-only research with read_only_task or read_only_skill. You must not write files, run unsafe shell commands, install capabilities, mutate memory, delegate to writer-capable sub-agents or skills, control long-lived processes, or mark execution steps complete. Before planning, if a decision that is genuinely the user's — tech stack, an ambiguous requirement, scope, an irreversible choice — would materially shape the plan and you can't settle it from the codebase or a sensible default, use the ask tool to clarify it first; otherwise pick the obvious default and state the assumption in the plan instead of asking. Then present a LAYERED plan as your reply and stop. Structure the plan as a two-level markdown list so it becomes a layered task list: each PHASE is a top-level numbered list item (a coherent milestone, e.g. "1. Add the config loader"), and each phase's concrete, verifiable sub-steps are bullets indented beneath it (e.g. "   - parse the TOML into Config"). Use plain numbered list items for phases — do NOT write phases as markdown headings (##, ###) — so both levels parse. Keep phases few (about 2-6). The user will be asked to approve before any changes are made.]
```

### 中文翻译

> [Plan 模式 —— 仅规划。你可以调研代码库与网络、用 `ask` 工具澄清问题、用 `todo_write` 维护规划状态、用 `read_only_task` 或 `read_only_skill` 派发独立的只读研究任务。你**不得**写文件、不得执行不安全的 shell 命令、不得安装能力（capability）、不得改写记忆（memory）、不得派发给具备写权限的子代理或技能、不得控制长生命周期进程、也不得把执行步骤标为完成。规划之前，如果存在一个**真正属于用户**的决策 —— 技术栈、含糊的需求、范围、不可逆的选择 —— 它会**实质性**影响计划，且你无法从代码库或合理的默认值里把它确定下来，那就先用 `ask` 工具澄清；否则就选明显的默认值，并在计划中**写明该假设**，而不是去问。随后把一份**分层计划**作为你的回复输出，**然后停下**。计划要写成两级 markdown 列表，从而构成一份分层任务列表：每个**阶段（PHASE）**是顶层的有序列表项（一个完整的里程碑，例如 "1. 添加配置加载器"），每个阶段下的具体、可验证的子步骤是缩进的无序项（例如 "   - 把 TOML 解析进 Config"）。阶段必须用普通的有序列表项 —— **不要**用 markdown 标题（`##`、`###`）来写阶段 —— 这样两级才能都被解析。阶段数量保持精简（大约 2–6 个）。任何改动落地前都会先请用户审批。]

### 设计动机

- **白名单显式列出**："ask、todo_write、read_only_task / read_only_skill 可用"——和 `TestPlanModeMarkerMatchesPolicy` 联动，保证 marker 描述与 `planmode.Policy` 实际白名单一致。
- **明确禁止 markdown heading**：因为后续 `planmode.ParsePlan` 会按 `1. … - …` 解析层级；用 `##` 会让任务列表平铺、丢失阶段。

---

## 3.2 `autoPlanClassifierPrompt`

| 元信息 | 值 |
| --- | --- |
| **常量名** | `autoPlanClassifierPrompt` |
| **来源文件** | [`internal/control/auto_plan_classifier.go`](../../internal/control/auto_plan_classifier.go) |
| **何时注入** | `auto_plan` 配置为 `on` 时，对每个新用户输入跑一次小型轮次（`max_tokens=80`，`temperature=0`），由独立 provider 给出 `{needs_plan, reason}` JSON。 |
| **模型契约** | 严格 JSON：`{"needs_plan":true|false,"reason":"short reason"}` |
| **作用** | 决定"这次请求要不要先进 plan 模式" —— 把 multi-step 实现 / 重构 / 跨文件 / spec 类工作分到 plan，把解释 / 单步直接命令分到非 plan。 |

### 原文

```
You classify whether a coding-agent user request should first enter read-only planning mode.
Return ONLY JSON: {"needs_plan":true|false,"reason":"short reason"}.
Use true for multi-step implementation, refactors, migrations, unclear cross-file work, PRD/spec/issue work, or tasks needing investigation before edits.
Use false for explanations, simple questions, single obvious edits, direct commands, or requests that should be answered without changing files.
```

### 中文翻译

> 你的职责是判断一条编程代理的用户请求**是否**应该先进入只读规划模式。
> 仅返回 JSON：`{"needs_plan":true|false,"reason":"短原因"}`。
> **true** 用于：多步骤实现、重构、迁移、跨文件且范围不清晰的工作、PRD/spec/issue 类工作，或需要在动手编辑之前先做调研的任务。
> **false** 用于：解释类问题、简单问答、单一且明显的编辑、直接命令，或那些不应该改动文件就能回答的请求。

### 用户回合格式

由代码拼成两个消息：

```
[system] autoPlanClassifierPrompt
[user]   heuristic_score=<int>

         USER_REQUEST:
         <原始用户输入>
```

`heuristic_score` 来自纯本地启发式（关键词、行数、问号比例等），分类器把它当作弱先验参考。

---

## 3.3 `planApprovedMessage`

| 元信息 | 值 |
| --- | --- |
| **常量名** | `planApprovedMessage` |
| **来源文件** | [`internal/control/controller.go`](../../internal/control/controller.go) |
| **何时注入** | 用户在 UI 点了"批准计划"后，controller 不让用户重新输入，而是替他发出这条用户回合，触发执行循环。 |
| **作用** | 关闭 plan 模式 + 直接告诉模型"按串行 todo_write / complete_step 工作流走"。 |

### 原文

```
Plan approved — plan mode is off; you’re cleared to make the changes without asking again. Implement the plan now. Use this serial workflow: 1) mark the first sub-step in_progress with todo_write (this establishes the task list); 2) execute the sub-step; 3) call complete_step with evidence — the host then marks that sub-step completed and moves the next one to in_progress for you. Repeat 2–3 for each remaining sub-step. You don’t need another todo_write to mark steps completed; each complete_step advances the list. Sign off one sub-step at a time — never batch multiple completions.
```

### 中文翻译

> 计划已批准 —— plan 模式已关闭；你被授权进行改动，不必再询问一次。**现在就实施这份计划**。采用如下串行工作流：1) 用 `todo_write` 把第一个子步骤标为 in_progress（这一步会建立任务列表）；2) 执行该子步骤；3) 用 `complete_step` 附上证据 —— 然后 host 会替你把该子步骤标为 completed，并把下一项搬到 in_progress。对剩余的每个子步骤重复 2–3。你**不需要**再调一次 `todo_write` 去标记完成；每次 `complete_step` 都会推进列表。一次只签收一个子步骤 —— **绝不**把多步完成合并成一次。

### 设计动机

- **"host then marks that sub-step completed"**：把"`complete_step` 一调，host 自动推进任务列表"这个隐含合约写给模型，避免它每完成一步都重新 `todo_write` 一次（造成多余编辑、缓存抖动）。
- **"never batch multiple completions"**：抑制模型一口气标多步完成、跳过证据记录。

---

## 3.4 附录：`legacyPlanModeMarker`（向后兼容剥离）

| 元信息 | 值 |
| --- | --- |
| **常量名** | `legacyPlanModeMarker` |
| **来源文件** | [`internal/control/input.go`](../../internal/control/input.go) |
| **何时使用** | **不**再注入。`StripComposePrefixes` 在恢复旧会话用户消息显示文本时，会把这个老 marker 与 3.1 的当前 `PlanModeMarker` 一起从历史里剥掉。 |
| **作用** | 兼容 v0.x 时代以 `[Plan mode — read-only. …]` 起头的老 plan 模式 marker；它指向的策略与 3.1 已不完全一致（白名单更窄、措辞更老），仅用于**识别并剥除**，不会再被发送给模型。 |

### 原文

```
[Plan mode — read-only. Explore the codebase first (read_file, ls, grep, glob, web_fetch, task, ask are available; writers are refused by the harness). Before planning, if a decision that is genuinely the user's — tech stack, an ambiguous requirement, scope, an irreversible choice — would materially shape the plan and you can't settle it from the codebase or a sensible default, use the ask tool to clarify it first; otherwise pick the obvious default and state the assumption in the plan instead of asking. Then present a LAYERED plan as your reply and stop — do not write files, edit, or run side-effecting bash. Structure the plan as a two-level markdown list so it becomes a layered task list: each PHASE is a top-level numbered list item (a coherent milestone, e.g. "1. Add the config loader"), and each phase's concrete, verifiable sub-steps are bullets indented beneath it (e.g. "   - parse the TOML into Config"). Use plain numbered list items for phases — do NOT write phases as markdown headings (##, ###) — so both levels parse. Keep phases few (about 2-6). The user will be asked to approve before any changes are made.]
```

### 备注

- 与 3.1 的差异：旧 marker 的白名单是 `read_file, ls, grep, glob, web_fetch, task, ask`；当前 marker 改为 `read_only_task, read_only_skill, todo_write, ask`，并且**显式列出禁止项**（写文件、不安全 bash、安装能力、改写 memory、派发可写子代理 / skill、控制长生命周期进程、把执行步骤标完成）。
- 历史会话恢复时，UI 会通过 `stripComposeMarker(s, legacyPlanModeMarker)` 把它从用户消息显示文本中移除，避免在聊天气泡里显示这段 marker 散文。
