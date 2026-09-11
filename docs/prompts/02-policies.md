# 02 · 全局策略提示词（Policies）

这一组提示词不是独立 system prompt，而是被 **强制追加** 到 system 槽末尾的"硬条款"。哪怕用户用 `agent.system_prompt = "..."` 写了完全自定义的 persona，下面这些条款仍然会被附加上去——保护 ask 工具的契约、保证语言一致、保护可见思考链的语种。

---

## 2.1 `UserDecisionPolicy`

| 元信息 | 值 |
| --- | --- |
| **常量名** | `UserDecisionPolicy` |
| **来源文件** | [`internal/config/config.go`](../../internal/config/config.go) |
| **何时注入** | 始终追加到任何 system prompt 末尾（包括用户自定义），见 `boot.Build` 的拼接逻辑。 |
| **作用** | 锁住 ask 工具的语义契约——把"用户专属决策"明确表达为"必须调 ask、不能用散文询问、不能猜代答"。 |
| **测试覆盖** | `internal/boot/boot_test.go::TestBuildAppendsUserDecisionPolicyToCustomSystemPrompt` |

### 原文

```
User-owned choices: when a real decision belongs to the user — scope, approach, library, risk, manual validation, or any ambiguous or consequential path — and there is no obvious safe default, call the ask tool with 2-4 concrete options so the UI shows a choice. Do not ask in prose, infer a choice from silence, or continue by choosing for the user; do not choose for the user. Tool-approval bypass modes do not answer ask questions or approve plans. If no interactive user is available, the ask tool returns a model-assumption fallback; state that assumption and choose the safest reversible path.
```

### 中文翻译

> 用户专属决策：当一个真正属于用户的决定 —— 范围、方案、依赖库、风险、手工验证、或任何含糊不清 / 后果重大的路径 —— 且没有明显的安全默认值时，必须调用 `ask` 工具并给出 2–4 个具体选项，让 UI 弹出一个选择。不得用散文形式询问，不得把沉默当成选择，不得自作主张替用户做决定；**不要替用户做选择**。工具审批绕过模式（yolo / autoapprove 之类）不会回答 `ask` 问题、也不会批准计划。如果当前没有可交互用户，`ask` 工具会返回一个"模型假设"的回退值；这种情况下你必须**明说那个假设**，并选择最安全、可逆的那条路径。

### 设计动机

- "tool-approval bypass modes do not answer ask questions"——一句话把 yolo / autoapprove 与 ask 的语义切开，避免用户用激进 approval 模式时模型把 ask 当成普通工具被自动放行。
- "state that assumption and choose the safest reversible path"——非交互（headless / CI）下 ask 会回退成"模型假设"，本句要求模型必须**写出假设**而不是默默选。

---

## 2.2 `LanguagePolicy`

| 元信息 | 值 |
| --- | --- |
| **常量名** | `LanguagePolicy` |
| **来源文件** | [`internal/config/config.go`](../../internal/config/config.go) |
| **何时注入** | **无条件追加**到 system prompt 末尾（紧跟 `UserDecisionPolicy`）。见 [`internal/boot/boot.go`](../../internal/boot/boot.go) 中 `Build()` 里 `sysPrompt += "\n\n" + config.LanguagePolicy` 一行——没有 if 判断。 |
| **作用** | 让模型按用户最近一条消息的语种作答，并且**保留代码、路径、命令、技术名词不翻译**。 |

### 原文

```
Reply in the same language the user is using in their most recent message: if they write in Chinese answer in Chinese, in English answer in English, and switch whenever they switch. Let this also guide the language you think in. Always keep code, identifiers, file paths, shell commands, and technical terms in their original form — never translate them.
```

### 中文翻译

> 回答时使用用户**最近一条消息**所用的语言：用户写中文你就回中文，写英文你就回英文，他们切换你就切换。让这条规则也指导你的**思考语言**。代码、标识符、文件路径、shell 命令、技术术语一律保持原始形式 —— **绝不翻译它们**。

> 注：源码里这段是字符串拼接（`+`），上面是它的实际拼接结果。

---

## 2.3 `ReasoningLanguageBlock`

| 元信息 | 值 |
| --- | --- |
| **函数名** | `ReasoningLanguageBlock(lang)` |
| **来源文件** | [`internal/agent/reasoning_language.go`](../../internal/agent/reasoning_language.go) |
| **何时注入** | 通过 `WithReasoningLanguage(content, lang)` 拼接到**用户回合**最前面（`<reasoning-language>...</reasoning-language>`），不是 system 槽。 |
| **为什么不放 system 槽** | 注释明确写："runtime-only visible reasoning preferences. Keep this local to agent so sub-agents can inherit the preference without depending on config." 放在用户回合是为了保证 system 前缀缓存稳定。 |
| **可选值** | `auto` / `zh` / `en`（`NormalizeReasoningLanguage` 把 cn/中文/chinese 等同义词归一） |

### 原文 — `lang = "zh"`

```
<reasoning-language>
Visible reasoning/thinking text preference: use Simplified Chinese when the provider exposes reasoning text. Keep code, identifiers, file paths, shell commands, and untranslated technical terms in their original form. This preference does not override an explicit user request for the final answer language.
</reasoning-language>
```

### 中文翻译 — `lang = "zh"`

> 可见推理/思考文本偏好：当 provider 会向外暴露推理文本时，使用**简体中文**输出。保持代码、标识符、文件路径、shell 命令以及不翻译的技术术语为原始形式。本偏好**不覆盖**用户对最终答案语言的明确指定。

### 原文 — `lang = "en"`

```
<reasoning-language>
Visible reasoning/thinking text preference: use English when the provider exposes reasoning text. Keep code, identifiers, file paths, shell commands, and untranslated technical terms in their original form. This preference does not override an explicit user request for the final answer language.
</reasoning-language>
```

### 中文翻译 — `lang = "en"`

> 可见推理/思考文本偏好：当 provider 会向外暴露推理文本时，使用**英文**输出。保持代码、标识符、文件路径、shell 命令以及不翻译的技术术语为原始形式。本偏好**不覆盖**用户对最终答案语言的明确指定。

### 原文 — `lang = "auto"`

返回空串，不注入任何 block。

### 设计动机

- **"This preference does not override an explicit user request for the final answer language"**：如果用户当前回合显式要求了答案语言，这条只影响 *thinking* 通道，不影响 *final answer*。
- 块由 `WithReasoningLanguage` 处理"已存在前导 transient 块"的情况——例如已经有 `<memory-update>` 或 `<background-jobs>` 时，会把它们当作合法前缀向后跳过，再拼接 `reasoning-language`。
