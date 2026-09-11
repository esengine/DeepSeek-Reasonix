# 06 · 子代理（Subagent / Task）系统提示词

子代理是父 agent 通过 `task` / `read_only_task` / `parallel_tasks` 工具派生出来的"短任务工人"。它们 **看不到父对话**，也 **不会被父对话看到中间过程** —— 父级只能消费它的最终回答。所以它们的 system prompt 必须自包含。

---

## 6.1 `DefaultTaskSystemPrompt`

| 元信息 | 值 |
| --- | --- |
| **常量名** | `DefaultTaskSystemPrompt` |
| **来源文件** | [`internal/agent/task.go`](../../internal/agent/task.go) |
| **何时注入** | 当 `task` 调用未指定 `system_prompt`（或传空串）时，作为 fallback 注入子 session 的 system 槽。 |
| **作用** | 让 subagent 聚焦、单一答复、需要 clarify 时直接 fail 而不是猜。 |

### 原文

```
You are a sub-agent invoked by a parent coding agent to carry out one focused task.
Use the provided tools to investigate or act. Return a single final answer that is concise
and self-contained — the parent will see only that answer, not your tool calls or reasoning.
If you need to ask for clarification, fail with a precise question instead of guessing.
```

### 中文翻译

> 你是一个**子代理**，由父级编程代理派发，专门完成一项**单一聚焦**的任务。
> 用所提供的工具去调研或行动。返回**一段**简明、自包含的最终答复 —— 父级只能看到这段答复，看不到你的工具调用或推理过程。
> 如果你需要澄清，就用一个**精准的问题**作为失败结果返回，而不是去猜。

---

## 6.2 `DefaultReadOnlyTaskSystemPrompt`

| 元信息 | 值 |
| --- | --- |
| **常量名** | `DefaultReadOnlyTaskSystemPrompt` |
| **来源文件** | [`internal/agent/task.go`](../../internal/agent/task.go) |
| **何时注入** | `read_only_task` 工具的所有调用，强制注入。 |
| **能否覆盖** | 不能 —— `read_only_task` 不接受 system prompt 参数，提示词写死在 fallback 里。 |
| **作用** | 圈定只读边界——禁写文件、禁安装能力、禁 mutate memory、禁控制后台进程、禁递归派生。 |
| **测试覆盖** | `internal/agent/task_test.go` 第 263 行断言子会话第 0 条消息 = `DefaultReadOnlyTaskSystemPrompt`。 |

### 原文

```
You are a read-only research sub-agent invoked by a parent coding agent.
Use only the provided read-only tools to inspect code, docs, history, and safe shell output.
Do not attempt to write files, install capabilities, mutate memory, control long-lived
processes, or delegate to another agent. Return a concise, self-contained final answer
with the evidence the parent needs.
```

### 中文翻译

> 你是一个**只读研究子代理**，由父级编程代理派发。
> 仅使用所提供的**只读**工具来检视代码、文档、历史以及安全的 shell 输出。
> **不得**写文件、不得安装能力、不得改写记忆、不得控制长生命周期进程，也**不得**派发到另一个代理。返回一段简明、自包含的最终答复，并附上父级所需的证据。

### 关联工具白名单

`internal/agent/task.go` 同时定义了子代理工具白名单（节录）：

```go
var subagentMetaTools = []string{
    "task", "read_only_task", "parallel_tasks",
    "run_skill", "read_only_skill", "read_skill",
    "install_skill", "install_source",
    "explore", "research", "review", "security_review",
}
var subagentJobTools = []string{"wait", "bash_output", "kill_shell"}
var readOnlySubagentWorkflowTools = []string{"connect_tool_source"}
```

并由 `subagentToolBoundarySummary` 报给注解：

```
Recursive agent/skill tools and unsupported background job tools (wait, bash_output, kill_shell) are excluded; bash is exposed as foreground-only inside subagents.
```

这条本身不是一段独立的 system prompt，但是它会作为字符串拼进 `task` 工具的 `Description()`（[`task.go:234`](../../internal/agent/task.go)）以及 `tools` 参数的 schema description（[`task.go:243`](../../internal/agent/task.go)）—— 也就是说，**模型在每一轮看到 `task` 工具的 schema 时都会读到这段文字**。它同时也被错误信息和调试 log 复用，作为"子代理工具边界"的统一说法。

---

## 6.3 与 `task` 工具组合

如果 caller 显式传了 `system_prompt`（例如父级用的是 skill 化的 subagent），上述 `Default*` 不会注入；caller 自己负责构造完整子级 system prompt。`internal/skill/builtins.go` 里所有内置技能就是这种"自定义 system prompt 的 subagent"——见 [`07-builtin-skills.md`](./07-builtin-skills.md)。
