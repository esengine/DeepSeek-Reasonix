# Reasonix Prompts 全集

本目录收集 Reasonix 仓库内所有面向 LLM 的 **system / instruction prompt**。所有内容均原样从源码常量与字符串提取，未做改写。每个 prompt 都标注了：

- **来源文件** — 仓库相对路径与符号名（点击可跳转同仓库源码文件）
- **作用** — 这条 prompt 在产品里承担的职责
- **触发时机** — 它何时被注入到模型请求里
- **原文** — 与代码中字符常量一字不差的内容

## 目录

| 分类 | 文件 | 说明 |
| --- | --- | --- |
| 主体（base / persona） | [`01-default-system-prompt.md`](./01-default-system-prompt.md) | 默认主 agent 系统提示词（DefaultSystemPrompt） |
| 全局策略（policy 拼接） | [`02-policies.md`](./02-policies.md) | UserDecisionPolicy、LanguagePolicy、ReasoningLanguageBlock |
| Plan / Approval 模式 | [`03-plan-mode.md`](./03-plan-mode.md) | Plan-mode marker、auto-plan classifier、planApprovedMessage |
| Goal 模式 / AutoResearch | [`04-goal-mode-and-autoresearch.md`](./04-goal-mode-and-autoresearch.md) | activeGoalBlock、autoResearchGoalInstructions、goalContinueTurn / goalSelfCheckTurn |
| 双模型协调 | [`05-coordinator-handoff.md`](./05-coordinator-handoff.md) | DefaultPlannerPrompt、formatHandoff、executor handoff retry |
| 子代理（subagent / task） | [`06-task-subagents.md`](./06-task-subagents.md) | DefaultTaskSystemPrompt、DefaultReadOnlyTaskSystemPrompt |
| 内置技能（builtin skills） | [`07-builtin-skills.md`](./07-builtin-skills.md) | explore / research / review / security-review / test / init / install-capability |
| 历史压缩 | [`08-compaction.md`](./08-compaction.md) | summarySystemPrompt（compact 总结） |
| 容错重试 | [`09-retry-and-recovery.md`](./09-retry-and-recovery.md) | streamRecoveryMessage、emptyFinalRetryMessage、finalReadinessRetryMessage |
| 工具表面控制 | [`10-token-economy.md`](./10-token-economy.md) | tokenEconomyPrompt |
| 服务器内部 prompt | [`11-server-and-utility.md`](./11-server-and-utility.md) | titlePrompt、conductor、prometheusPrompt |
| 自定义 slash 命令 | [`12-custom-commands.md`](./12-custom-commands.md) | `.reasonix/commands/*.md` 用户级模板 |
| 输出风格（persona 切换） | [`13-output-styles.md`](./13-output-styles.md) | 内置 explanatory / learning / concise；KeepCoding 行为 |

## 阅读建议

1. **先看 `01` + `02`**：理解 Reasonix 在不修改用户提示词的情况下，永远附加哪些"硬约束"。
2. **`03` + `04` 是会话级模式切换层**：`/plan` 进入只读讨论模式、`/goal` 进入跨回合自治模式，二者都直接增量改变会话提示词。
3. **再看 `05`–`06`**：了解 Coordinator/Subagent 任务委派如何换上各自的 system prompt 而保持前缀缓存稳定。
4. **`07` 是内置工具**：所有 `/explore`、`/research`、`/review` 等 slash 命令的实际行为，都由这些 prompt 决定。
5. **`08`–`10` 是健壮性层**：上下文压缩、流中断恢复、token 经济模式三类提示词。
6. **`11`–`13`** 收录边角的小型提示词、用户可扩展点与 persona 切换层。

## 相关源码

- 主提示词常量与拼接：[`internal/config/config.go`](../../internal/config/config.go)
- Agent 主循环（注入 retry / recovery 文本）：[`internal/agent/agent.go`](../../internal/agent/agent.go)
- 计划模式策略：[`internal/planmode/policy.go`](../../internal/planmode/policy.go)
- Plan/Auto-Plan 分类器：[`internal/control/auto_plan_classifier.go`](../../internal/control/auto_plan_classifier.go)
- 双模型协调：[`internal/agent/coordinator.go`](../../internal/agent/coordinator.go)
- 子代理：[`internal/agent/task.go`](../../internal/agent/task.go)
- 上下文压缩：[`internal/agent/compact.go`](../../internal/agent/compact.go)
- 内置技能：[`internal/skill/builtins.go`](../../internal/skill/builtins.go)
- token 经济模式：[`internal/boot/token_profile.go`](../../internal/boot/token_profile.go)
- Reasoning language：[`internal/agent/reasoning_language.go`](../../internal/agent/reasoning_language.go)
- HTTP 服务端 title prompt：[`internal/serve/serve.go`](../../internal/serve/serve.go)
- 业务 controller：[`internal/control/controller.go`](../../internal/control/controller.go)
- 输出风格：[`internal/outputstyle/outputstyle.go`](../../internal/outputstyle/outputstyle.go)
- Goal 模式（Compose 注入 active-goal 块）：[`internal/control/input.go`](../../internal/control/input.go)
- Goal FSM 与伪用户回合：[`internal/control/goal.go`](../../internal/control/goal.go)

> ⚠ 这些提示词会随版本演化。本文档基于当前仓库快照生成；如需校对，请以 `internal/` 下的最新源码为准。
