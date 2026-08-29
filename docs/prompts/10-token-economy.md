# 10 · Token 经济模式提示词

`internal/boot/token_profile.go` 实现了"token 经济模式"——可选的精简工具表面，把不常用的工具源（skills / MCP / LSP / web_fetch / install_source / task / read_only_task）藏到 `connect_tool_source` 工具背后，按需启用。这条线由一段提示词驱动模型理解"现在是经济模式，要按需开工具"。

---

## 10.1 `tokenEconomyPrompt`

| 元信息 | 值 |
| --- | --- |
| **常量名** | `tokenEconomyPrompt` |
| **来源文件** | [`internal/boot/token_profile.go`](../../internal/boot/token_profile.go) |
| **何时注入** | `Agent.token_mode = "economy"` 时，由 `boot.Build` 把这段附加到 system prompt 末尾（在 `UserDecisionPolicy` 之后）。 |
| **作用** | 告知模型当前默认工具表是"瘦身版"，需要 skills / MCP / LSP / web_fetch / install / task / read_only_task 时要主动调用 `connect_tool_source`。 |

### 原文

```
Token economy mode is on. Keep the default tool surface lean. Optional sources are hidden behind connect_tool_source; enable skills, read_only_skill, MCP servers, LSP, web_fetch, install_source, task, or read_only_task only when the current request actually needs them.
```

### 中文翻译

> 当前已开启 **token 经济模式**。保持默认工具表**精简**。可选工具源被藏在 `connect_tool_source` 之后；只有在**当前请求**确实需要时，才启用 skills、`read_only_skill`、MCP servers、LSP、`web_fetch`、`install_source`、`task` 或 `read_only_task`。

### 配套：核心工具白名单

`tokenEconomyCoreBuiltins` —— 经济模式默认依然暴露的内置工具：

```
bash, bash_output, code_index, complete_step, edit_file, glob, grep,
kill_shell, ls, move_file, multi_edit, read_file, todo_write, wait, write_file
```

### `connect_tool_source` 工具描述

虽然不是 system prompt，但模型决策"开哪个 tool source"时会读到这条工具描述：

```
Token economy mode only: enable an optional tool source when the task needs it. Sources: skills, read_only_skill, mcp, lsp, web_fetch, install_source, task, read_only_task. For mcp, pass the configured server name; omit name to list servers. Newly enabled tools are available on the next model request.
```

#### 中文翻译

> 仅在 token 经济模式下生效：当任务确有需要时启用一个可选工具源。可选源有：`skills`、`read_only_skill`、`mcp`、`lsp`、`web_fetch`、`install_source`、`task`、`read_only_task`。对于 `mcp`，传入已配置的 server 名；省略 name 即列出所有 server。新启用的工具会在**下一次模型请求**时生效。

### 设计动机

- "Newly enabled tools are available on the next model request" —— 防止模型期待"开了立刻能用"。新启用的工具确实需要等下一轮 schema 一起送过去，前缀缓存才不会被破坏。
