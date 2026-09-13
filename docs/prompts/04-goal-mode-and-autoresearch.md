# 04 · Goal 模式与 AutoResearch

`/goal` 命令把当前会话切换到"目标模式"——controller 会在每个用户回合的**用户消息最前面**注入一段 `<active-goal>...</active-goal>` 块，告诉模型"现在按目标自治推进，不要描述完计划就停"。如果该 goal 命中 AutoResearch 启发式，块里还会再追加一份"长程研究协议"（约 1KB）。

Goal 模式还会注入两条**伪用户回合**——`goalContinueTurn`（继续推进）与 `goalSelfCheckTurn`（终结前自检），它们都不是真正的用户消息，但在历史里以 user 角色出现，所以也归入"提示词"。

---

## 4.1 `<active-goal>` 块（Compose 注入）

| 元信息 | 值 |
| --- | --- |
| **构造函数** | `activeGoalBlock(goal, researchMode)` in [`internal/control/input.go`](../../internal/control/input.go) |
| **何时注入** | `Controller.Compose(text)` 中：当 `goals.snapshot()` 报告 goal 非空且 `status == GoalStatusRunning` 时，把这块拼到 `text` 最前面（`PlanModeMarker` 之前、`<reasoning-language>` 等其它 transient 块之前）。 |
| **作用** | 让模型在跨回合内**自治**推进同一个目标，并强制它每条 assistant 回复以 `[goal:continue]` / `[goal:complete]` / `[goal:blocked:<reason>]` 三个状态标记之一**单独成行**结尾。 |

### 块结构（伪模板）

```
<active-goal>
{用户在 /goal 命令里输入的目标文本}

Goal mode: pursue this goal autonomously. Keep working across turns until the goal is complete. Prefer sensible defaults over asking the user; use ask only when you are truly blocked on a user-owned decision. Do not stop after describing a plan; execute the next useful step. End every goal-mode assistant reply with exactly one status marker on its own line: [goal:continue], [goal:complete], or [goal:blocked:<short reason>].

{当 shouldUseAutoResearch 为 true 时，此处再追加 4.2 的 autoResearchGoalInstructions}
</active-goal>
```

### 固定文本原文（中段）

```
Goal mode: pursue this goal autonomously. Keep working across turns until the goal is complete. Prefer sensible defaults over asking the user; use ask only when you are truly blocked on a user-owned decision. Do not stop after describing a plan; execute the next useful step. End every goal-mode assistant reply with exactly one status marker on its own line: [goal:continue], [goal:complete], or [goal:blocked:<short reason>].
```

### 中文翻译

> **Goal 模式**：按目标**自治**推进。跨回合不断工作，直到目标完成。**优先选择合理的默认值**，而不是反复询问用户；仅当真正被一个**用户专属决策**卡住时才用 `ask`。**不要**在描述完计划就停下；**执行**下一步有用的工作。每条 goal-mode 的 assistant 回复**结尾**必须只放一个状态标记并单独成行：`[goal:continue]`、`[goal:complete]` 或 `[goal:blocked:<short reason>]`。

### 设计动机

- 把"goal 文本"放到块**首**而非块尾——保证模型每轮都能"看见目标本身"，而不是只看到一堆 mode 指令。
- 三种状态标记是 controller 端的**机器协议**：`parseGoalStatusMarker` 会扫到末尾最后一行非空字符串去 match，错位 / 多写都会被识别为"未声明状态" → goal 继续运转。

---

## 4.2 `autoResearchGoalInstructions`

| 元信息 | 值 |
| --- | --- |
| **常量名** | `autoResearchGoalInstructions` in [`internal/control/input.go`](../../internal/control/input.go) |
| **何时注入** | `shouldUseAutoResearch(goal, mode)` 为 `true` 时，紧跟在 4.1 中段之后塞进同一个 `<active-goal>` 块。 |
| **触发条件** | `--research / --auto-research / --deep` 显式开启；或 goal 文本命中 `autoResearchStrongKeywords`（如"持续/长期/彻底/直到根因/long-horizon/keep researching/root cause/thoroughly"等）；或六大类 phase 关键词命中 ≥ 4 类。 |
| **关闭条件** | `/goal --simple` 或 `/goal --no-research`。 |
| **作用** | 把 long-horizon 研究 / 调试 / 优化 / 实现型任务**结构化**到磁盘上的 `.reasonix/autoresearch/<task-id>/`，强制 stale_count 升级与 pivot 协议。 |

### 原文

```
AutoResearch protocol: this goal looks like long-horizon research, debugging, optimization, or implementation work. Treat AutoResearch as a durable strategy for this Goal, not as a background daemon or a global skill.
- Say briefly in the first visible reply that the goal is being handled with AutoResearch and that state will live under .reasonix/autoresearch/<task-id>/.
- Keep dynamic state out of REASONIX.md, AGENTS.md, project memory, system prompts, and tool schemas. Use project-local .reasonix/autoresearch/ state only.
- For a new task, create a collision-resistant task id YYYYMMDD-HHMMSS-slug, check .reasonix/autoresearch/ first, and append -2, -3, etc. only on collision. Reuse an explicitly supplied .reasonix/autoresearch/<task-id>/ path exactly.
- Maintain state/task_spec.md, state/progress.json, state/findings.jsonl, state/directions_tried.json, state/iteration_log.jsonl, and logs/heartbeat.jsonl. Record goal, scope, non-goals, allowed operations, success criteria, verification gates, iteration direction, evidence, stale_count, pivots, blockers, and completion summary.
- Before each iteration, read the existing state files as authoritative, append a heartbeat, choose a direction that differs materially from directions already tried, execute the smallest evidence-producing chunk, verify it, then persist JSON/JSONL state before reporting.
- Increment stale_count when an iteration lacks accepted evidence or repeats a prior direction. At stale_count >= 2, make a structural pivot such as changing evidence source, entrypoint, implementation boundary, test oracle, benchmark, decomposition, environment, platform, or refutation angle. At stale_count >= 4, stop autonomous digging and ask for the smallest external input needed.
- Workers or subagents may gather evidence, but the orchestrator owns canonical state writes. Workers must not publish, push, delete, contact external systems, or write canonical state unless explicitly designated.
- Complete only after auditing every success criterion in task_spec.md against direct evidence. Public publishing, destructive changes, credential use, payments, external notifications, privacy-sensitive output, and cache-sensitive changes still require the normal Reasonix gates.
```

### 中文翻译（要点版）

> **AutoResearch 协议**：本 goal 看起来是长程的研究 / 调试 / 优化 / 实现类工作。请把 AutoResearch 视为**该 Goal 的持久策略**，而不是后台守护进程或全局 skill。
> - 在**第一条可见回复**里**简短说明**：这个目标正在用 AutoResearch 处理，状态将存放在 `.reasonix/autoresearch/<task-id>/` 之下。
> - **动态状态**不要写进 `REASONIX.md`、`AGENTS.md`、项目 memory、system prompts 或工具 schema；只用 project-local 的 `.reasonix/autoresearch/` 目录。
> - 新任务：用 `YYYYMMDD-HHMMSS-slug` 形式构造 task id；先扫一下 `.reasonix/autoresearch/`，仅在**确有冲突**时再追加 `-2`、`-3` 等。如果用户**显式指定**了路径，**精确**复用它。
> - 维护这些文件：`state/task_spec.md`、`state/progress.json`、`state/findings.jsonl`、`state/directions_tried.json`、`state/iteration_log.jsonl`、`logs/heartbeat.jsonl`。记录目标、范围、非目标、允许的操作、成功标准、验证门、迭代方向、证据、`stale_count`、pivot、阻塞、完结总结。
> - **每次迭代前**：把已有状态文件作为**权威**读入；追加一条 heartbeat；选择与"已尝试方向"实质不同的新方向；执行**最小**的"能产出证据"的 chunk；验证它；然后**先持久化** JSON/JSONL 状态，再做汇报。
> - 当一次迭代缺乏被接受的证据，或者重复了一个先前的方向时，把 `stale_count` 加 1。`stale_count >= 2` 触发**结构化 pivot**（更换证据源、入口、实现边界、测试 oracle、benchmark、分解方式、环境、平台或反证角度）；`stale_count >= 4` 停止自治深挖，并向用户索要**最小必要的外部输入**。
> - Worker / subagent **只能采集证据**；规范状态的写入归 orchestrator 所有。Worker 在未被显式授权时**不得**发布、push、删除、联系外部系统或写规范状态。
> - 仅当 `task_spec.md` 中**每一条**成功标准都对照直接证据审过一遍后才声明 complete。**公开发布、破坏性改动、用 credential、付款、对外通知、隐私敏感输出、缓存敏感改动**仍需走 Reasonix 既有的审批门。

### 设计动机

- 把策略**写在 prompt 里**而不是塞进 `REASONIX.md` —— 因为 AutoResearch 是 per-goal 的临时姿态；写进 memory 会污染未来所有 session。
- `stale_count >= 2 → pivot`、`>= 4 → 停手并向用户求助` —— 强制把"在原地反复试错"这一最常见失败模式变成显式协议。

---

## 4.3 `goalContinueTurn`（伪用户回合）

| 元信息 | 值 |
| --- | --- |
| **常量名** | `goalContinueTurn` in [`internal/control/goal.go`](../../internal/control/goal.go) |
| **何时注入** | goal FSM 在 `advance` 后判断需要"继续推进"（无 notice、无 intercept）时，作为**下一次用户回合的伪文本**送入。 |
| **作用** | 让模型在没有真正用户输入的情况下也能继续推进 goal；同时**温和提醒**它如何用三种状态标记结尾。 |

### 原文

```
Continue pursuing the active goal. If it is complete, provide the concise final result and end with [goal:complete]. If it is truly blocked on a user-owned decision after trying sensible defaults, end with [goal:blocked:<short reason>]. Otherwise do the next useful work and end with [goal:continue].
```

### 中文翻译

> 继续推进当前的 active goal。如果**已完成**，给出简明的最终结果，并以 `[goal:complete]` 结尾。如果在尝试过合理的默认值之后，**确实**被一个用户专属决策**卡住**，以 `[goal:blocked:<short reason>]` 结尾。否则，**做下一步有用的工作**，并以 `[goal:continue]` 结尾。

---

## 4.4 `goalSelfCheckTurn`（strict 模式专属伪回合）

| 元信息 | 值 |
| --- | --- |
| **常量名** | `goalSelfCheckTurn` in [`internal/control/goal.go`](../../internal/control/goal.go) |
| **何时注入** | `/goal --strict` 模式下，模型首次发出 `[goal:complete]` 且所有 todos 也都已 done 后，FSM 把 `selfCheckDone = true` 并以这条伪回合再驱动一轮 —— 在真正终结 goal 之前**强制做一次质量自检**。 |
| **作用** | 把"声明完成"和"实际过验证"分成两步，避免模型口头上喊 `[goal:complete]` 而漏跑测试 / 漏验证。 |

### 原文

```
The agent signaled goal completion and all tasks are marked done. Before finalizing, perform a brief quality self-check:
1. Verify any changed files compile or parse correctly
2. Run the relevant tests if applicable
3. Confirm the original requirements are met
If everything checks out, signal [goal:complete]. If issues are found, fix them and signal [goal:complete] when done.
```

### 中文翻译

> agent 已发出 goal 完成信号，且所有 task 已被标为 done。**最终敲定之前**，先做一次简短的**质量自检**：
> 1. 检查所有被改动的文件是否能正常编译 / 解析；
> 2. 如适用，跑一下相关测试；
> 3. 确认原始需求**已被满足**。
> 一切通过就发出 `[goal:complete]`。若发现问题，**先修掉**，修完再发 `[goal:complete]`。

---

## 4.5 关联：`formatIncompleteTodos` 注入文本

非 prompt 常量，但同样以"伪用户回合"形式出现的还有当 `[goal:complete]` 到达却存在未完成 todo / readiness 失败时的拦截文本，由 `formatIncompleteTodos` 在 [`internal/control/goal.go`](../../internal/control/goal.go) 内动态生成，开头固定为：

```
Goal signaled complete but issues remain:
- the following tasks are still incomplete:
  - <todo title> (<status>)
  …
- <executor.GoalReadinessFailure() 的额外 readiness 原因>
Fix or use todo_write/complete_step to mark done, then [goal:complete] again.
```

它会被 `IsSyntheticUserMessage` 通过 `"Goal signaled complete but issues remain:"` 前缀识别，从聊天 UI 中过滤掉。同样位于这一族的还有 idle 检测（`maxGoalIdleTurns >= 2`）触发的：

```
No tool calls in recent turns. Either make progress with tools or signal [goal:blocked:<reason>].
```

—— 这些都不是 `const` 常量，但也都会以 user-role 消息进入对话历史，按"伪用户回合"看待即可。

---

## 4.6 与其他章节的关系

- **`<active-goal>` 块的位置**与 `03-plan-mode.md` 的 `PlanModeMarker` **互斥但同源**：`Compose()` 先拼 active-goal，再拼 plan marker；plan 模式 + goal 模式可以同时开（先研究后执行）。
- **`syntheticPrefixes` 列表**：见 [`internal/control/input.go`](../../internal/control/input.go) 的 `IsSyntheticUserMessage`，里面已显式列出 4.3 / 4.4 / 4.5 的开头前缀以便聊天 UI 不把这些"伪用户回合"渲染成用户气泡（issue #3653）。

---

## 附录 A · Compose-time transient blocks（由 `Controller.Compose` 注入）

除了 4.1 的 `<active-goal>` 块和 03 章的 `PlanModeMarker` 之外，[`Controller.Compose`](../../internal/control/input.go) 在每条**用户回合**（含 synthetic 用户回合）的最前面**还可能**追加另外三种"transient block"。它们不是 system prompt（不进入缓存稳定的前缀），但确实会被模型读到，且都直接影响模型对该回合的解析。

### A.1 `<memory-update>` 块（项目记忆变更通知）

| 元信息 | 值 |
| --- | --- |
| **构造点** | [`Controller.Compose` 中 `notes := c.memory.drainPending()` 分支](../../internal/control/input.go) |
| **何时注入** | 用户在本次会话内通过 `/remember`、`# <note>` 快捷写入或编辑了 `REASONIX.md`/项目 memory 后，**仅紧跟着的下一个用户回合**会带上这一块；之后被吸收进 system 前缀，不再附加。 |
| **作用** | 让模型**当回合就感知**最新的项目记忆，而不必等到下一次 session（缓存重建）才生效。 |

#### 原文（块结构）

```
<memory-update>
The following project-memory changes were just made and apply from now on:
- {note 1}
- {note 2}
...
</memory-update>

{用户原始消息}
```

#### 中文翻译

> `<memory-update>` 项目记忆刚刚发生了如下变更，从此刻起生效：
> - {条目 1}
> - {条目 2}
> ……
> `</memory-update>`

#### 设计动机

- 不写进 system 前缀 → 不破坏 prompt cache。
- 用 XML 块包裹 → 模型可以与"用户原始消息"清晰分隔。
- 一次性 drain → 同一条 memory 不会被反复注入。

### A.2 `<background-jobs>` 块（后台任务完成通知）

| 元信息 | 值 |
| --- | --- |
| **构造点** | [`Controller.Compose` 中 `c.jobs.DrainCompletedNoteForSession(...)` 分支](../../internal/control/input.go) |
| **何时注入** | 自上一回合以来有 `bash` 后台 job、子代理、或并行任务完成时，把它们的"完成纪要"在本回合用户消息**最前面**追加一次（drain 一次性消费）。 |
| **作用** | 后台 job 的完成通知本来只走 UI Notice 通道、不进入模型上下文；这一块把它**显式**喂给模型，让 `wait` / `bash_output` / 子代理结果在没被主动 poll 的情况下也能被察觉。 |

#### 原文（块结构）

```
<background-jobs>
{c.jobs.DrainCompletedNoteForSession(...) 返回的 note，多行文本}
</background-jobs>

{用户原始消息}
```

#### 中文翻译

> `<background-jobs>` ……（后台任务完成纪要原文）…… `</background-jobs>`

> 注意：该 note 内部的文本由 jobs 子系统自行格式化，不在 prompt 文档管辖范围内（结构会随 job 类型变化）。

### A.3 `Referenced context:` 前置段（@-引用展开）

| 元信息 | 值 |
| --- | --- |
| **构造点** | [`Controller.ResolveRefs`](../../internal/control/controller.go)、[`cli/chat_tui.go`](../../internal/cli/chat_tui.go) |
| **何时注入** | 用户输入里包含 `@路径`、`@资源` 或拖入图片时，先把每个引用展开成 `<file>`、`<dir>`、`<resource>`、`<image>` XML 块，整体以 `Referenced context:` 头部前置在用户原始文本前。 |
| **逆向解析** | [`StripReferencedContextPrefix`](../../internal/control/input.go) 在生成会话 title / 预览时把这段剥掉，让显示文本回归用户**真正**键入的内容（issue #4954）。 |

#### 原文（块结构）

```
Referenced context:

<file path="...">...</file>
<dir path="...">...</dir>
<resource ref="...">...</resource>
<image path="...">...</image>

{用户原始消息}
```

#### 中文翻译

> 引用的上下文：
>
> `<file path="...">…</file>`
> `<dir path="...">…</dir>`
> `<resource ref="...">…</resource>`
> `<image path="...">…</image>`
>
> {用户原始消息}

#### 设计动机

- 把"用户引用的素材"和"用户的话"**结构化分隔**，避免模型把文件内容当成用户的指令。
- 展开后的内容才进 prompt cache，路径本身不进 → 同一文件多次引用复用 cache。
- 标签名固定为 `<file> / <dir> / <resource> / <image>`，便于 `StripReferencedContextPrefix` 在多处（title、preview、UI 显示）一致地剥掉。

### A.4 注入顺序

`Compose()` 实际拼装顺序是（从外到内、从最先 prepend 到最后 prepend）：

1. （先有）`<active-goal>...</active-goal>`（4.1 / 4.2）
2. （次之）`PlanModeMarker`（03 章）
3. （再）`<reasoning-language>...</reasoning-language>`（02.3）
4. （再）`<memory-update>...</memory-update>`（A.1）
5. （最后）`<background-jobs>...</background-jobs>`（A.2）
6. 用户原始消息（如果含 `@` 引用，则用户消息内部已先被 `Referenced context:` 包过一层 —— A.3 的注入位于 `Compose` **之前**，由 `ResolveRefs` 完成）

也就是说，模型实际看到的一个"重武装"用户回合从外到内是：

```
<background-jobs>...</background-jobs>

<memory-update>...</memory-update>

<reasoning-language>...</reasoning-language>

[plan-mode marker]

<active-goal>
...
</active-goal>

Referenced context:
<file ...>...</file>
...

{用户原始消息}
```

所有 transient block 都不进入缓存稳定的 system 前缀，因此**新增 / 删除任意一块都不会让 prompt cache 失效**。
