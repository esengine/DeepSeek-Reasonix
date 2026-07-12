# 子智能体 Profile

子智能体 Profile 是可复用、显式调用的专用智能体，适合代码评审、问题调查、文档整理等
聚焦任务。每个 Profile 都是带有 `runAs: subagent` 的手动 Skill：Reasonix 会启动隔离的
子智能体，把 Profile 提示词和任务交给它执行，并且只把最终答案返回父智能体。

桌面端、交互式 CLI 和 Headless CLI 共用这些 Profile。它们直接复用现有 Skill 文件格式和
目录，不引入独立数据库。

## 创建 Profile

从提示词文件创建项目级 Profile：

```bash
reasonix subagent create reviewer \
  --description "检查改动的正确性和回归风险" \
  --prompt-file reviewer.md \
  --tools read_file,grep,bash \
  --model deepseek-pro \
  --effort high
```

在 workspace 中，`create` 默认使用 project scope；不在 workspace 中时默认使用 global
scope。也可以通过 `--scope project` 或 `--scope global` 明确指定。项目级 Profile 存放在
`.reasonix/skills/<name>/SKILL.md`，全局 Profile 存放在 Reasonix home 的 Skill 目录中，
具体路径见[配置路径](./CONFIG_PATHS.zh-CN.md)。

提示词可来自 `--prompt`、`--prompt-file PATH`、`--prompt-file -` 或标准输入：

```bash
printf '%s\n' '检查任务，只报告可执行的问题。' | \
  reasonix subagent create reviewer --description "代码评审"
```

名称可以包含字母、数字、`_`、`-` 和 `.`。如果名称已经被项目级、全局、自定义或内置 Skill
占用，Reasonix 会拒绝创建，避免覆盖已有内容。

## 调用 Profile

在交互式 CLI 或桌面聊天中使用斜杠命令：

```text
/reviewer 评审当前 diff
```

这会真正启动隔离子智能体，并非把提示词文本注入父智能体。父会话只保留任务和子智能体的
最终答案，不保留子智能体的完整工作上下文。

脚本和其他 Headless 场景应使用显式命令：

```bash
# 使用只读工具预览。
reasonix subagent try reviewer "评审当前 diff"

# 按正常权限和沙盒策略运行。
reasonix subagent run reviewer "评审并修复当前 diff"

# 从标准输入读取任务，并限制工具调用轮次。
git diff | reasonix subagent run reviewer --max-steps 20
```

`run`/`try` 的参数应放在任务文本之前。两个命令都支持 `--model REF` 和 `--dir PATH`。
`try` 始终选择只读 runner；`run` 使用正常的隔离 runner，权限中的 `deny` 规则和沙盒限制
仍然有效。普通 `reasonix run` 仍是单次任务入口，不会隐式解释 `/<profile>` 语法。

## 管理 Profile

```text
reasonix subagent list [--dir PATH]
reasonix subagent create <name> --description TEXT (--prompt TEXT | --prompt-file PATH)
  [--scope project|global] [--model REF] [--effort LEVEL]
  [--tools a,b] [--color NAME] [--dir PATH]
reasonix subagent edit <name> [--description TEXT]
  [--prompt TEXT | --prompt-file PATH] [--model REF] [--effort LEVEL]
  [--tools a,b] [--color NAME] [--dir PATH]
reasonix subagent delete <name> --yes [--dir PATH]
reasonix subagent try <name> [--model REF] [--max-steps N] [--dir PATH] <task>
reasonix subagent run <name> [--model REF] [--max-steps N] [--dir PATH] <task>
```

`edit` 只修改命令行中显式提供的字段。用显式空值清除可选字段：

```bash
reasonix subagent edit reviewer --model= --effort= --tools= --color=
```

省略工具列表或将其清空，表示 Profile 不额外添加工具白名单；runner 原有的工具可用性、权限、
沙盒和只读规则仍然有效。`delete` 必须带 `--yes`，不会发生隐式删除。

内置 Profile 没有可写的 Skill 文件。对它们执行 `edit` 时只支持 `--model` 和 `--effort`，
保存的位置与桌面设置页使用的按 Profile 覆盖配置相同；传入空值会删除对应覆盖。

## 文件格式与高级 Profile

CLI 和桌面 Profile 编辑器会生成精简的 Skill 文件：

```yaml
---
name: reviewer
description: 检查改动的正确性和回归风险
color: orange
invocation: manual
runAs: subagent
model: deepseek-pro
effort: high
allowed-tools: [read_file, grep, bash]
---
你是专注的代码评审员。检查指定改动，只返回可执行的问题，并按严重程度排序。
```

`invocation: manual` 表示模型不会从固定 Skill 索引中自动发现该 Profile，但用户仍可显式
调用。`allowed-tools` 是 Profile 级工具白名单，不能绕过权限系统。

也可以手写更丰富的 `runAs: subagent` Skill，例如使用自定义 Skill path 或额外 frontmatter。
这些 Profile 可以被列出和调用，但 Profile 编辑器会拒绝编辑或删除以下内容：

- 不属于 project/global scope 的 Profile；
- `invocation` 不是 `manual` 的 Profile；
- 含有编辑器无法管理的 frontmatter 的文件；
- 含有 `references/` 或 `scripts/` 目录的 Skill。

这样可以防止精简编辑器静默丢弃高级 Skill 内容。此类 Profile 应直接按 Skill 文件管理。

## 模型与推理强度选择

有效模型和推理强度按以下优先级选择：

1. `agent.subagent_models` 和 `agent.subagent_efforts` 中按 Profile 设置的覆盖；
2. Profile frontmatter 中的 `model` 和 `effort`；
3. `agent.subagent_model` 和 `agent.subagent_effort` 默认值；
4. 已配置的 executor/默认模型及其默认推理强度。

例如：

```toml
[agent]
subagent_model = "deepseek-pro"
subagent_effort = "high"
subagent_models = { reviewer = "deepseek/deepseek-v4-pro" }
subagent_efforts = { reviewer = "max" }
```

`subagent run` 或 `subagent try` 的 `--model` 参数用于选择该 Headless 命令初始化时的默认
模型；Profile 专属配置仍按上述优先级生效。

## 桌面端同步与排障

桌面设置页和 `reasonix subagent create` 创建的 Profile 共用同一批文件。修改 Profile 后，
请刷新或新建会话，让已经运行的会话重新加载 Skill registry。

如果调用时报 Profile 未知或已禁用，请检查 `reasonix subagent list`、当前 `--dir` 和
`skills.disabled_skills`。如果编辑时报 custom 或 rich Profile，应直接编辑其 `SKILL.md`，
不要强行经过 Profile 编辑器。Reasonix 在解析有效模型时会拒绝未知模型引用和无效 effort。
