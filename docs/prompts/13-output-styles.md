# 13 · 输出风格（Output Styles）

`internal/outputstyle/outputstyle.go` 定义了一个**可选的 persona / 语气切换层**——在不重写默认 system prompt 的前提下，让用户用 `/output-style <name>` 把一段额外的 persona body **追加**到（或**整段替换**）`DefaultSystemPrompt`。

它和 `01-default-system-prompt.md` 的 base prompt 一样**会真实进入 system 槽**，且发生在记忆 / skill 索引拼接**之前**，所以在 `KeepCoding=false` 这种"整段替换"形态下，连 `02-policies.md` 的硬条款也会落到 base 之后再追加。

---

## 13.1 注入流程一览

`internal/boot/boot.go` 中的 `Build()` 会按下面的顺序产出最终 system prompt：

```
sysPrompt = ResolveSystemPromptForRoot()        // base，默认是 DefaultSystemPrompt
if outputstyle.Resolve(...) ok:                 // ← 12 章这一步
    sysPrompt = outputstyle.Apply(sysPrompt, st)
sysPrompt += "\n\n" + UserDecisionPolicy        // 02 章
sysPrompt += "\n\n" + LanguagePolicy            // 02 章
if tokenEconomy:
    sysPrompt += "\n\n" + tokenEconomyPrompt    // 09 章
sysPrompt = memory.Compose(sysPrompt, mem)      // REASONIX.md / AGENTS.md
if !tokenEconomy:
    sysPrompt = skill.ApplyIndex(sysPrompt, skills)  // 06 章索引
```

`Apply(base, st)` 的具体行为：

| `st.KeepCoding` | `st.Body` | 结果 |
| --- | --- | --- |
| `true`（追加） | 非空 | `base + "\n\n" + body` |
| `true` | 空白 | 不变 |
| `false`（替换） | 非空 | `body`（base 整段被丢弃） |
| `false` | 空白 | 不变 |

所有 3 个 builtin 的 `KeepCoding` 都是 `true`——它们是**追加**型，不是替换型。只有用户在 `~/.reasonix/output-styles/<name>.md` 里写一份 `keep-coding-instructions: false` 的自定义 style，才会触发"整段替换"。

> 注：自定义 style 文件落在 `~/.reasonix`/`~/.agents`/`~/.agent`/`~/.claude` 任一约定目录下的 `output-styles/` 子目录，前/后置项目根 + frontmatter（`name`、`description`、`keep-coding-instructions`）。

---

## 13.2 `explanatory`

| 元信息 | 值 |
| --- | --- |
| **变量** | `builtins[0]` in [`internal/outputstyle/outputstyle.go`](../../internal/outputstyle/outputstyle.go) |
| **`Description`** | `Explain non-obvious implementation choices as you go` |
| **`KeepCoding`** | `true`（追加） |
| **使用场景** | 用户希望模型边写代码边讲清"为什么这么写"——折中、被否决的备选、关键 trade-off。 |

### 原文

```
Communication style — Explanatory: as you work, surface the reasoning behind non-obvious choices. After a substantive change, add a short "## Insight" note covering the key trade-off or why an alternative was rejected. Teach the why, not just the what; keep it brief.
```

### 中文翻译

> 沟通风格 —— **Explanatory**：在你工作时，把非显而易见的选择背后的**理由**摆出来。每做完一次实质性改动后，**追加一段简短的 `## Insight`** 注记，覆盖关键 trade-off 或某个备选为何被否决。讲**为什么**，而不只是讲**做了什么**；保持简短。

---

## 13.3 `learning`

| 元信息 | 值 |
| --- | --- |
| **变量** | `builtins[1]` |
| **`Description`** | `Collaborate and leave TODO(human) stubs for the user to complete` |
| **`KeepCoding`** | `true`（追加） |
| **使用场景** | "教学型" pair-programming：模型把最具学习价值的小段留给用户自己实现。 |

### 原文

```
Communication style — Learning: work collaboratively rather than doing everything. When a meaningful implementation decision comes up, pause and ask the user to make the call. For the most instructive pieces, write the surrounding code but leave a small, clearly-marked `TODO(human)` stub with a one-line description for the user to implement themselves.
```

### 中文翻译

> 沟通风格 —— **Learning**：以**协作**为主，而不是大包大揽。遇到有意义的实现决策时，**暂停**并请用户来决定。对最具教学价值的那些片段，把周围的代码写好，但留下一个小而**清晰标注**的 ``TODO(human)`` 占位（带一行描述），由用户自己实现。

---

## 13.4 `concise`

| 元信息 | 值 |
| --- | --- |
| **变量** | `builtins[2]` |
| **`Description`** | `Terse replies: minimal prose, code and bullets only` |
| **`KeepCoding`** | `true`（追加） |
| **使用场景** | 终端 / 自动化场景下要求极简输出：少废话、多代码、多要点。 |

### 原文

```
Communication style — Concise: keep replies terse. No preamble or postamble, no restating the request. Prefer code and short bullet points over paragraphs; answer in the fewest words that are still clear.
```

### 中文翻译

> 沟通风格 —— **Concise**：让回复**简短**。不要开场白也不要收尾语，不要复述请求。**多用代码与短要点**而非段落；用仍然清楚的最少字数作答。

---

## 13.5 `default` 与"无 style"

`Resolve("")` 与 `Resolve("default")` 都返回 `ok=false`，`Apply` 不会被调用 —— 也就是说**不存在**一个名为 `default` 的 style body：那是"什么都不追加，原样使用 `DefaultSystemPrompt`"的语义。文档 01 章给出的就是这种"无 style"形态。
