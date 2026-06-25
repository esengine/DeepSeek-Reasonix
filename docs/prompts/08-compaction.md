# 08 · 历史压缩（Compaction）提示词

当一个 session 的累积 prompt 触达 `compact_ratio`（默认 0.8）阈值，agent 会用 executor 模型自身**对自己**做一次"压缩 → 摘要"调用。这个压缩调用临时换上一个**专门的 system prompt**，让模型把旧对话折叠成一段结构化简报。

---

## 8.1 `summarySystemPrompt`

| 元信息 | 值 |
| --- | --- |
| **常量名** | `summarySystemPrompt` |
| **来源文件** | [`internal/agent/compact.go`](../../internal/agent/compact.go) |
| **何时注入** | `Agent.compact` 触发时，临时构造一次单轮请求：`[system] summarySystemPrompt + [user] 待折叠的旧消息` |
| **Timeout** | 单次压缩请求受 `summaryTimeout = 90s` 限制——卡住时直接报错并退化成机械折叠（保留近 N 条）。 |
| **作用** | 让模型按固定的七节模板（Standing facts、Goal、Decisions、Files & code、Commands、Errors、Pending）输出，便于后续轮次把摘要当事实检索。 |

### 原文

```
You are compacting the earlier part of a coding agent's conversation to save context.
The agent keeps your summary alongside the user's own turns (kept verbatim) and the recent tail; your job is to fold the assistant/tool work into a briefing it can resume from.
Write under these exact headings, omitting a heading only if it has no content:

## Standing facts & constraints
Everything the user stated that still governs the work — names, paths, IDs, versions, tokens, preferences, and hard "never do X" rules — in their own words. Be exhaustive; this is the durable contract, so prefer over- to under-including.

## Goal
The user's request and intent.

## Decisions & rationale
Key choices made so far and why — so they are not re-litigated or reversed.

## Files & code
Files read or modified, with the specific facts that matter: signatures, line locations, data shapes, and exact edits applied. Be concrete; this is what lets the agent act without re-reading everything.

## Commands & outcomes
Commands run (builds, tests, git) and their relevant results — what passed, what failed, and the error text that matters.

## Errors & fixes
Problems hit and how they were resolved (or not), so the same dead ends are not repeated.

## Pending & next step
What is still in progress or unstarted, and the single most concrete next action to take.

Rules: be terse — bullet points and fragments, not prose. Preserve identifiers, paths, and numbers exactly. Do NOT invent anything not present in the messages; if something is unknown, leave it out rather than guessing.
```

### 中文翻译

> 你正在压缩一段编程代理的早期对话以节省上下文。
> 代理会把你的摘要保存下来，与用户**逐字保留**的发言以及最近一段尾部一并使用；你的工作是把 assistant / tool 那些工作折叠成一份**可续作**的简报。
> 用以下**精确的**小节标题来写，没有内容的小节就**省略**该标题：
>
> `## Standing facts & constraints`
> 用户表述过、且至今仍约束着工作的所有内容 —— 名称、路径、ID、版本、token、偏好、以及"绝不做 X"这类硬规则 —— **用他们自己的措辞**。要尽量穷尽；这是持久契约，宁可多写也不要漏。
>
> `## Goal`
> 用户的请求与意图。
>
> `## Decisions & rationale`
> 至今做出的关键选择以及理由 —— 防止它们被反复重新讨论或被推翻。
>
> `## Files & code`
> 读过或改过的文件，附上**重要事实**：函数签名、行号位置、数据结构、已应用的精确编辑。要具体；这是让代理无需重读一切就能继续行动的依据。
>
> `## Commands & outcomes`
> 跑过的命令（构建、测试、git 等）及其相关结果 —— 哪些通过、哪些失败、关键报错文本。
>
> `## Errors & fixes`
> 撞到过的问题以及如何解决（或仍未解决），让相同的死胡同**不要**重走。
>
> `## Pending & next step`
> 仍在进行或尚未启动的项；以及**单一一条**最具体的下一步动作。
>
> 规则：**简练** —— 用要点和短句，不用散文。**精确**保留标识符、路径、数字。**不要**编任何消息中没有的内容；未知的就留空，**不要猜**。

### 配套：摘要回写为伪用户消息的三种包装

`summary` 字符串本身只是模型按 7 节模板写出的纯文本；真正塞回 session 时，会被包成一条 `role=user` 的伪消息。**根据触发路径不同，包装文本有三种形态**（都会进入模型上下文，并被 [`internal/control/input.go`](../../internal/control/input.go) 中的 `syntheticPrefixes` 识别为"伪用户回合"，避免聊天 UI 把它们渲染成用户气泡）：

#### 8.1.1 自动压缩（`Agent.compact`，`compact_ratio` 阈值触发）

最常见路径，使用 `<compaction-summary>` XML 标签包裹（常量 `summaryTagOpen` / `summaryTagClose`，定义于 [`internal/agent/compact.go:40`](../../internal/agent/compact.go)）：

```
<compaction-summary>
Summary of earlier conversation (older messages were compacted to save context):
{model-generated summary body}
</compaction-summary>
```

> 中文翻译：`<compaction-summary>` 早先对话的摘要（更早的消息已被压缩以节省上下文）：……`</compaction-summary>`

允许用户随时 `unfold` / 检查它，IDE 端也按 XML 标签做折叠展示。

#### 8.1.2 手动 `/summarize-from`（`Agent.SummarizeFrom`）

把"从 fromIdx 开始到末尾"的区段折叠成一条用户消息——**不加 XML 标签**，仅有前缀（[`compact.go:303`](../../internal/agent/compact.go)）：

```
Summary of the later conversation (compacted from here on):
{model-generated summary body}
```

> 中文翻译：之后对话的摘要（自此处起被压缩）：……

#### 8.1.3 手动 `/summarize-up-to`（`Agent.SummarizeUpTo`）

把"系统提示后到 toIdx 之前"的早段折叠——同样**不加 XML 标签**，仅有前缀（[`compact.go:335`](../../internal/agent/compact.go)）：

```
Summary of earlier conversation (compacted up to here):
{model-generated summary body}
```

> 中文翻译：早先对话的摘要（压缩至此处）：……

> ⚠ 8.1.2 / 8.1.3 是手动 slash 触发的"区段折叠"，**不会**附加 `<compaction-summary>` 标签，因此聊天 UI 不能按 XML 边界折叠这两段——它们靠 [`syntheticPrefixes`](../../internal/control/input.go) 中的明文前缀匹配做"非用户气泡"判定。如果你修改任一前缀字符串，**两处都要改**，否则会导致这些伪回合被误渲染为用户消息。

### 设计动机

- **七个固定 heading** —— 抗模型偏好漂移：不同模型/不同温度下，自由格式摘要内容会"飘"，而固定大节让 prompt-cache 友好（结构稳定）+ 让后续轮次能 grep。
- **"Preserve identifiers, paths, and numbers exactly"** —— 编程上下文里，把 `internal/agent/agent.go:1429` 改写成"the agent file"会直接破坏接下来的 read_file/diff 命中率。
- **"Do NOT invent anything not present in the messages"** —— 反幻觉硬约束。
