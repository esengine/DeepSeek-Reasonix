## 问题描述

`detectHashMemory` 会将任何以 `#` 开头的消息（不是 `##` 或 `\#`）视为记忆笔记，直接写入 `REASONIX.md`，不经过模型处理。

但在讨论 GitHub issue 时，`#123` 或 `#42 这个 bug 在哪个版本？` 这类消息是常见的 issue 引用格式。当前这些消息会被 hash-memory 拦截，用户看到的是：

```
▸ 已记录（项目）— 追加到 /path/to/REASONIX.md
```

而非正常的模型回复。

## 复现步骤

1. 在聊天中输入 `#123`
2. 消息未发给模型，而是被追加到 REASONIX.md

## 期望行为

`#` 后紧跟数字（GitHub issue/PR 引用风格）的消息应当正常通过，不拦截为记忆笔记。

| 输入 | 当前 | 期望 |
|---|---|---|
| `#123` | 拦截→笔记 | 放行→正常聊天 |
| `#42 这个问题需要修复` | 拦截→笔记 | 放行→正常聊天 |
| `#记个笔记` | 拦截→笔记 | 不变（仍记笔记）|
| `#` 或 `#   ` | 放行（null）| 不变 |
| `## heading` | 放行（null）| 不变 |

## 技术细节

检测函数在 src/cli/ui/hash-memory.ts:29-59。需要在 `##` 检测之后、`#g` 检测之前添加一条规则：`#` 后紧跟数字的不作笔记处理。

---

## Problem

The `detectHashMemory` function treats any message starting with `#` (that isn't `##` or `\#`) as a memory note and writes it to REASONIX.md without sending it to the model.

However, when discussing GitHub issues, `#123` or `#42 which version has this bug?` are common issue reference formats. These messages get falsely intercepted by hash-memory, showing:

```
▸ noted (project) — appended to /path/to/REASONIX.md
```

## Expected behavior

Messages starting with `#` followed by digits (GitHub issue/PR reference style) should pass through to normal chat instead of being captured as memory notes.
