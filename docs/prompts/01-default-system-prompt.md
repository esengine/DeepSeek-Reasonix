# 01 · 默认系统提示词

主 agent 启动时落入 system 槽的"persona / 行为契约"。它和后面 [`02-policies.md`](./02-policies.md) 里的两条全局策略一起，构成了缓存稳定的**前缀**——只要用户没有在配置里写 `agent.system_prompt` 或 `agent.system_prompt_file` 覆盖，每个会话都会用它开头。

---

## 1.1 `DefaultSystemPrompt`

| 元信息 | 值 |
| --- | --- |
| **常量名** | `DefaultSystemPrompt` |
| **来源文件** | [`internal/config/config.go`](../../internal/config/config.go)（`const DefaultSystemPrompt`） |
| **何时注入** | `Default()` 里写入 `Agent.SystemPrompt`；最终通过 `Config.ResolveSystemPromptForRoot` 落到 agent session 的 `system` 消息。 |
| **能否覆盖** | 可以——TOML `agent.system_prompt = "..."` 或 `agent.system_prompt_file = "..."` 即覆盖；但 `UserDecisionPolicy` 仍会被强制追加。 |
| **作用** | 给 Reasonix 一个"编码代理"基本人格：理解优先、用工具核实、最小可用变更、todo 明面化、plan 模式只读。 |

### 原文

```
You are Reasonix, a coding agent focused on executing code tasks.
Use the provided tools to read and write files and run shell commands.
Principles: understand the request before acting; verify with tools instead of
guessing; keep changes minimal and correct; briefly summarize what you did.
For multi-step work, track progress with the todo_write tool: lay out the steps,
keep exactly one in_progress, and flip each to completed as you finish it — update
the list as you go, not just at the end.
In plan mode the harness blocks writer tools: do read-only research, then write a
concise plan as your reply and stop. The user is asked to approve before anything
is changed; once approved, work through the steps, updating the task list as you go.
```

### 中文翻译

> 你是 Reasonix，一个专注于执行编码任务的代码代理（coding agent）。
> 使用所提供的工具来读写文件、运行 shell 命令。
> 准则：行动前先理解请求；用工具核实而非靠猜；保持改动最小且正确；简明地总结你做了什么。
> 对于多步骤的工作，使用 `todo_write` 工具跟踪进度：把步骤列出来，**始终保持有且仅有一个 in_progress**，每完成一项就把它翻成 completed —— 边做边更新这份列表，而不是只在最后才更新。
> 处于 plan 模式时，运行框架（harness）会屏蔽所有写入类工具：你只做只读式的调研，然后把简明的计划作为回复输出，**然后就停下**。系统会先请用户审批，审批通过后，再按步骤推进，并随做随更新任务列表。

### 设计动机摘录（来自周边代码注释）

- 必须**字节稳定**——这部分文字进入 DeepSeek 的自动前缀缓存，所以在一个 session 内不能被中途修改（详见 `REASONIX.md` "Cache-first" 一节）。
- "plan mode 写器被阻断"是 harness 级强约束，不是请求模型自觉；提示词只是把这个事实**告诉**模型，避免它假装能写文件。

---

## 1.2 用户自定义系统提示词时的拼接顺序

代码里 [`internal/boot/boot.go`](../../internal/boot/boot.go) 的 `Build()` 解析 system prompt 的实际拼接顺序为（以源码 `Build` 中的相邻语句为准）：

```
sysPrompt = ResolveSystemPromptForRoot()                 // 用户配置的 system_prompt 或 DefaultSystemPrompt
if outputstyle.Resolve(cfg.Agent.OutputStyle, …) ok:
    sysPrompt = outputstyle.Apply(sysPrompt, st)         // 见 12 章；KeepCoding=false 时整段替换 base
sysPrompt += "\n\n" + UserDecisionPolicy                 // 见 02 章；无条件追加
sysPrompt += "\n\n" + LanguagePolicy                     // 见 02 章；无条件追加
if tokenEconomy:
    sysPrompt += "\n\n" + tokenEconomyPrompt             // 见 09 章；仅 token-mode=economy 时追加
sysPrompt = memory.Compose(sysPrompt, mem)               // REASONIX.md / AGENTS.md 折叠
if !tokenEconomy:
    sysPrompt = skill.ApplyIndex(sysPrompt, skills)      // 见 07 章；skills 索引行
```

值得注意的是：

- `outputstyle.Apply` 在 `KeepCoding=false`（"replace" 型 style）情况下会**整段替换** base —— 但**不影响**它后面的 `UserDecisionPolicy / LanguagePolicy / memory / skills` 折叠：这些"硬条款 + 项目记忆 + 技能索引"始终落在最终 prompt 的末段。
- `LanguagePolicy` **不是**条件追加；不论 UI 语言能否解析都会拼上去。配合 [`02-policies.md`](./02-policies.md#22-languagepolicy)。
- `tokenEconomyPrompt` 仅在 `--token-mode economy` 这条路径上追加；非 economy session 不出现。详见 [`10-token-economy.md`](./10-token-economy.md)。

后续策略详见 [`02-policies.md`](./02-policies.md)、Output style 详见 [`13-output-styles.md`](./13-output-styles.md)。
