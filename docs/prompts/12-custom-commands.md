# 12 · 自定义 Slash 命令模板（用户/项目级）

Reasonix 兼容类 Claude Code 的自定义 slash 命令机制：把模板文件放进 `.reasonix/commands/`（项目级）或 `~/.config/reasonix/commands/`（用户级），文件名即命令名。运行时 `$1`、`$2` ... 替换为位置参数，`$ARGUMENTS` 替换为整段。

---

## 12.1 项目内置示例：`.reasonix/commands/review.md`

| 元信息 | 值 |
| --- | --- |
| **文件** | [`.reasonix/commands/review.md`](../../.reasonix/commands/review.md) |
| **触发** | 用户输入 `/review <path>`。 |
| **frontmatter 字段** | `description` 用于命令面板标题，`argument-hint` 用于 UI 提示。 |

### 原文

```markdown
---
description: Review a file for bugs
argument-hint: [path]
---
Read $1 and list any correctness bugs or risky patterns, with file:line references, most important first. Focus: $ARGUMENTS.
```

### 中文翻译

```markdown
---
description: 审查某个文件中的 bug
argument-hint: [path]
---
阅读 $1，列出其中所有正确性 bug 或有风险的模式，用 file:line 给出引用，**最重要的排在最前**。聚焦点：$ARGUMENTS。
```

> 注意：`/review` 命令名与内置 `/review` skill 同名时，**用户/项目级模板优先**，所以这条会覆盖内置那条。

---

## 12.2 模板替换规则（实现细节）

虽然不是提示词本身，但模板渲染规则决定了模型最终看到的 user-turn 文本，列在这里供撰写新模板时参考：

| 占位符 | 含义 |
| --- | --- |
| `$1` … `$9` | 用户输入按空白切分后的位置参数。空时替换为空串。 |
| `$ARGUMENTS` | 命令后面的整段（剥掉命令名本身），保留中间空白。 |
| frontmatter 之外的所有正文 | 逐字进入 user 回合（替换完占位符之后） |

### 用户级 vs 项目级优先级

```
.reasonix/commands/<name>.md            <-- 项目级，最高优先
~/.config/reasonix/commands/<name>.md   <-- 用户级
内置 builtin skill                       <-- 最低优先
```

—— 同名 override 顺序参见 `internal/skill/store.go` 与 `internal/command/` 包的相关读取逻辑。

---

## 12.3 MCP 服务器导出的 prompts（动态）

[`internal/plugin/prompts.go`](../../internal/plugin/prompts.go) 的 `Client.listPrompts` / `getPrompt` 把每个连接上的 MCP 服务器 `prompts/list` 接口暴露的提示词，注册成形如 `mcp__<server>__<prompt>` 的 slash 命令。

调用 `prompts/get` 时支持位置参数与命名参数（`PromptArg.Name / Required`），返回的 `messages[].content.text` 会被拼接成单段 user 回合发给主模型。

> 这一类 prompt 的具体内容由各个 MCP 服务器自定义，不存在仓库内静态可枚举的清单——本文档不内嵌它们的原文，只保留接入路径与格式说明。
