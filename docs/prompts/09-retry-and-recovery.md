# 09 · 容错与恢复（Retry & Recovery）提示词

当模型给出"不及格"的回答时（流被打断、最终答案为空、虚假声称只读、host 校验未通过等），agent 不会原样返回失败，而是**追加一条系统侧的"伪用户回合"**，再让模型重跑一轮。下面 4 段就是 4 类不及格场景对应的纠正词。

> 注意：这些都是 `func` 而非 `const`，因为它们的措辞偶尔依赖运行时上下文（如 `reason` 字段）。

---

## 9.1 `streamRecoveryMessage`

| 元信息 | 值 |
| --- | --- |
| **函数名** | `streamRecoveryMessage(hasPartialText, hadPartialTool bool) string` |
| **来源文件** | [`internal/agent/agent.go`](../../internal/agent/agent.go) |
| **何时注入** | provider 流被中断（网络断、超时、被 cancel）时，把已接收到的部分文本/工具调用作为现实，用这段提示词让模型续上。 |
| **三种分支** | 取决于"是否在切流前看到过可见正文"以及"是否在切流前开始过工具调用流"。 |

### 原文 — `hadPartialTool == true`

```
The previous assistant response was interrupted while a tool call was streaming. Continue the same task now. If a tool is still needed, issue a fresh complete tool call from scratch; do not rely on any partial tool-call arguments from the interrupted stream.
```

#### 中文翻译

> 上一条 assistant 回复在**工具调用流**进行到一半时被打断了。**现在继续同一个任务**。如果仍然需要某个工具，**重新**从头发起一个**完整的**工具调用；**不要**依赖被打断流里那些**残缺**的 tool-call 参数。

### 原文 — `hadPartialTool == false && hasPartialText == true`

```
The previous assistant response was interrupted during streaming. Continue the same task from immediately after the partial assistant message above. Do not repeat text that is already visible.
```

#### 中文翻译

> 上一条 assistant 回复在流式输出过程中被打断了。**从上面那段残缺 assistant 消息的紧后位置**继续同一个任务。**不要**重复已可见的文本。

### 原文 — 都为 false

```
The previous assistant response was interrupted during streaming before visible answer text was completed. Continue the same task now and provide the next useful response.
```

#### 中文翻译

> 上一条 assistant 回复在可见答复文本完成之前就被打断了。**现在继续同一个任务**，给出下一段有用的回复。

---

## 9.2 `emptyFinalRetryMessage`

| 元信息 | 值 |
| --- | --- |
| **函数名** | `emptyFinalRetryMessage()` |
| **来源文件** | [`internal/agent/agent.go`](../../internal/agent/agent.go) |
| **何时注入** | 模型用完一轮但未给出任何可见正文（典型是只产了 reasoning text、空 final）。配合 `maxEmptyFinalBlocks = 3`：连续三次还是空答，agent 直接终结一轮并报告。 |

### 原文

```
The previous assistant response finished without any visible answer text. Continue the same task now and provide a concise visible answer to the user. Do not send reasoning only.
```

### 中文翻译

> 上一条 assistant 回复结束时**没有任何可见答复文本**。现在继续同一个任务，并给用户一段简明、**可见**的答复。**不要只发推理（reasoning）**。

---

## 9.3 `finalReadinessRetryMessage(reason)`

| 元信息 | 值 |
| --- | --- |
| **函数名** | `finalReadinessRetryMessage(reason string) string` |
| **来源文件** | [`internal/agent/agent.go`](../../internal/agent/agent.go) |
| **何时注入** | host 端"final-answer readiness"检查失败时（典型：todo_write 还有未完成项就给 final），用 `reason` 注入具体原因。 |
| **测试覆盖** | `internal/agent/final_readiness_test.go::TestFinalReadinessRetryMessageKeepsUserChoicesInteractive`——断言提示词里同时含 `ask tool`、`concrete options`、`wait for its tool result` 等关键词。 |

### 模板（伪 fmt 拼接）

```
Host final-answer readiness check failed. Before giving a final answer, address the missing host-observable receipts: {reason}. Run the required tool calls, then answer when readiness is satisfied. If the blocked item needs user input, a user-owned choice, or manual review, call the ask tool with concrete options and wait for its tool result; do not ask in prose, and do not claim the user answered unless an actual ask tool result or a new user message says so.
```

### 中文翻译

> Host 端的最终答复就绪检查（final-answer readiness）未通过。在给最终答复**之前**，先补齐缺失的、host 可见的回执：`{reason}`。先把必要的工具调用都跑掉，等就绪条件满足后再回答。如果被卡住的那一项需要用户输入、用户专属选择或人工审查，就用**具体选项**调 `ask` 工具，并**等它的工具结果**；不要用散文形式提问，也**不要**在没有真正的 `ask` 工具结果或新一条用户消息的情况下声称用户已回答。

### 设计动机

- 这条提示词紧紧贴合 `UserDecisionPolicy`（见 [`02-policies.md`](./02-policies.md)）——它专门在"模型想跳过 ask 直接答"的位置补一刀。

---

## 9.4 `executorHandoffRetryMessage`

| 元信息 | 值 |
| --- | --- |
| **函数名** | `executorHandoffRetryMessage()` |
| **来源文件** | [`internal/agent/agent.go`](../../internal/agent/agent.go) |
| **何时注入** | executor 在 handoff 之后又复读 planner 的"我是只读、要交给 executor"一类话术时。 |

> 已在 [`05-coordinator-handoff.md`](./05-coordinator-handoff.md) 给出完整原文，本节不重复。

---

## 9.5 关联：`shouldNudgeExecutorHandoff` 触发判定

判断要不要发 9.4 的 retry，最终汇总到 `shouldNudgeExecutorHandoff(input, answer) = !executorHandoffAllowsTextOnly(input, answer)`（[`agent.go:1142`](../../internal/agent/agent.go)）。

后者会顺序检查三组词表，**任一命中都会让 `executorHandoffAllowsTextOnly` 返回 true，从而 `shouldNudgeExecutorHandoff` 返回 false，也就是阻止 9.4 retry 被注入**——它们是"放行 / 豁免"词，不是"触发"词。

### 9.5.1 真正的 deferral 短语 —— `executorHandoffDeferralPhrases`

| 含义 | 在模型 answer 里出现这些短语 = 模型在"嘴上接活、却没真正调工具"。 |
| --- | --- |
| **来源** | [`internal/agent/agent.go`](../../internal/agent/agent.go) `executorHandoffDeferralPhrases` 变量声明处 |
| **被消费在** | `looksLikeExecutorHandoffDeferral(answer)` —— **命中即视作 deferral，从而允许纯文本，进而阻止 9.4 retry**。 |

```
"plan looks", "looks good", "should be easy", "should be straightforward",
"i can implement", "i'll implement", "i will implement", "i'll get started",
"let me ", "i will now", "i'll now", "i can do that",
"计划看起来", "可以实现", "我会", "我将", "接下来我", "马上开始",
```

### 9.5.2 纯文本计划识别词 —— `executorHandoffTextOnlyPlanTerms`

| 含义 | 在 planner output 里出现这些短语 = 此次 handoff 的"计划"本身就只需要 executor 用文字回答用户，而非真的去调工具。 |
| --- | --- |
| **被消费在** | `handoffPlanLooksTextOnly(plan)`（前提：planner 的 plan 段中 **没有** `executorHandoffLocalActionTerms` 这样的本地动作词）。 |

```
"tell the user", "ask the user", "guide the user", "explain to the user",
"summarize", "summary", "tl;dr", "tldr", "answer the user", "respond to the user",
"provide guidance", "walk the user", "instruct the user", "have the user",
"user should", "the user should", "user can", "the user can", "manual", "manually",
"no tools needed", "no tool calls needed", "does not need tools", "needs no tools",
"listen", "play a song", "compare the difference", "checkbox",
"告诉用户", "询问用户", "问用户", "让用户", "请用户", "指导用户", "解释", "总结", "回答",
"手动", "无需工具", "不需要工具", "试听", "听歌", "对比", "勾选",
```

### 9.5.3 纯文本任务识别词 —— `executorHandoffTextOnlyTaskTerms`

同样作用在"放行"侧——出现这些原始用户任务词，配合 `executorHandoffWorkRequestTerms` 不命中，就把整段 handoff 视为只需要纯文本回答：

```
"now what", "what next", "tl;dr", "tldr", "summarize", "summary", "explain",
"i installed", "i just installed", "i turned on", "i enabled", "it's on", "it is on",
"怎么办", "下一步", "然后呢", "总结", "解释", "说明", "装了", "装好了", "安装了", "开了", "开启了", "打开了",
```

—— 这些都不是塞给模型的提示词，但决定了 9.4 retry 是否被**抑制**，所以一并附在这里方便交叉查阅。
