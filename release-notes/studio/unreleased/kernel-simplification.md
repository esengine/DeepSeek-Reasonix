## 变更

- 子代理统一为 `spawn_agents`，一次 1 到 8 个并行任务，子代理自带读文件、写文件、执行命令三件工具（ad2c96347）。
- 计划模式下子代理只读；完整输出归档，主模型先看开头和引用，再用 `read_subagent_result` 读后面（ad2c96347）。
- `complete_step` 不再要求提交证据，计划步骤不再有验收和检查字段（5766a8ebe）。
- `use_capability` 的说明不再提 `task:fleet`，工具清单前缀因此变化一次（ad2c96347）。

## 移除

- 移除 `task`、`fleet`、`parallel_tasks`、后台子任务、转向、`continue_from`、隔离工作树和多候选择优（ad2c96347）。
- 移除“完成标准”、交付验收卡片、就绪检查、评审闸和基线检查（b364c1693）。
- 移除缺失推理检测（be5c98c74）。
- 移除 `--fail-on-unverified` 和 `agent.host_verification`，旧配置里带这些键仍可加载（8c785fa4b）。
- 设置页移除“给运行中子代理的指示”（ad2c96347）。

---
<!-- lang:en -->

## Changed

- Sub-agents are now one `spawn_agents` tool: 1 to 8 parallel tasks, each with read file, write file and run command (ad2c96347).
- Sub-agents are read-only in plan mode; full output is archived and read in pages with `read_subagent_result` (ad2c96347).
- `complete_step` no longer asks for evidence and plan steps carry no acceptance or verification fields (5766a8ebe).
- The `use_capability` description no longer names `task:fleet`, so the tool prefix changes once (ad2c96347).

## Removed

- Removed `task`, `fleet`, `parallel_tasks`, background sub-tasks, steering, `continue_from`, isolated worktrees and best-of-n (ad2c96347).
- Removed completion criteria, delivery receipt cards, readiness checks, the review gate and baseline checks (b364c1693).
- Removed missing-reasoning detection (be5c98c74).
- Removed `--fail-on-unverified` and `agent.host_verification`; old configs carrying them still load (8c785fa4b).
- Removed "guidance to a running sub-agent" from Settings (ad2c96347).
