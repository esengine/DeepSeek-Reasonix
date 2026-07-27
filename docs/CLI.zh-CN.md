# Reasonix CLI 命令参考

<a href="../README.zh-CN.md">README</a>
&nbsp;·&nbsp;
<a href="./CLI.md">English</a>
&nbsp;·&nbsp;
<a href="./GUIDE.zh-CN.md">使用指南</a>

本文介绍交互式会话、一次性自动化、会话恢复、权限参数和常用会话内命令。Provider
配置、插件和沙盒策略见[使用指南](./GUIDE.zh-CN.md)。

## 启动会话

```sh
reasonix
reasonix --model deepseek-pro
reasonix --profile delivery --effort high
reasonix --dir /path/to/project
```

不带子命令运行 `reasonix` 会进入交互式终端界面。尚未配置 provider 时，先运行
`reasonix setup`。

| 参数 | 用途 |
| --- | --- |
| `--model NAME` | 选择已配置的 provider 或 `provider/model` 引用。 |
| `--profile economy\|balanced\|delivery` | 选择运行时工作模式。 |
| `--effort LEVEL` | 覆盖当前会话的 reasoning effort。 |
| `--max-steps N` | 为本次运行设置工具调用轮数上限；`0` 使用自动执行。 |
| `--dir PATH` | 加载配置和工具前切换 workspace 根目录。 |
| `--add-dir PATH` | 增加一个允许工具写入的目录；可重复传入。 |
| `-c`、`--continue` | 恢复最近一次会话。 |
| `-r`、`--resume [QUERY]` | 打开会话选择器，或恢复匹配的会话。 |
| `--copy` | 复制要恢复的会话，并在可写副本中继续。 |
| `--allowed-tools RULES` | 增加仅当前会话生效的权限 allow 规则；可重复传入，`--allowedTools` 是别名。 |
| `--permission-mode MODE` | 以指定的权限姿态启动。 |
| `--yolo` | 以 YOLO 模式启动；是 `--dangerously-skip-permissions` 的别名。 |

适用时，参数可以放在 prompt 前面或后面。

## 配置供应商

```sh
reasonix setup                    # 管理用户全局配置
reasonix setup --local            # 管理 ./reasonix.toml
reasonix setup /path/to/config.toml
```

在交互式终端中，`reasonix setup` 是一个暂存式供应商管理器。它会列出已配置的
provider，并支持：

- 添加 OpenAI-compatible 或 Anthropic-compatible provider；
- 编辑 endpoint 和模型列表；
- 更新 API Key，或测试连接并刷新模型；
- 设置默认模型；
- 删除 provider。

选择“保存并退出”后会先展示并确认待执行操作；取消会丢弃本次修改。保存时 setup 会重新
加载最新配置：桌面端或其他 CLI 产生的不相关修改会被保留，改到同一项时则报告冲突，
不会直接覆盖。

Provider 定义只保存 `api_key_env` 变量名。即使使用 `--local`，Key 的真实值也始终保存
在 CLI 与桌面端共用的 Reasonix 全局 `.env` 中。如果变量名已被其他 provider 使用，
setup 会询问是否共享该凭据；两个 provider 使用不同 Key 时，应改用不同变量名。通过
setup 添加或删除 provider 时，也会同步维护桌面端 provider access，因此相同模型可以
直接在桌面端使用。

## 一次性运行与自动化

脚本只需要最终回答时，使用 `-p` / `--print`：

```sh
reasonix -p "总结这个仓库"
reasonix -p "总结这个仓库" --output-format json
reasonix run "实现 main.go 里的 TODO"
echo "解释这段代码" | reasonix run
```

未使用 `-p` 或结构化输出格式时，`reasonix run` 保持正常的终端流式展示。它也接受
`--model`、`--profile`、`--max-steps`、`--effort`、`--dir`、`--add-dir`、
`--continue`、`--resume PATH`、`--copy`、`--allowed-tools` 和
`--permission-mode`。

### 输出格式

| 格式 | 行为 |
| --- | --- |
| `text` | 人类可读文本；配合 `-p` 时只输出最终回答。 |
| `json` | 输出一个最终结果对象。 |
| `stream-json` | 每行输出一个共用 `eventwire` JSON 对象，最后再输出最终结果对象。 |

```sh
reasonix -p "列出有风险的改动" --output-format text
reasonix -p "总结 diff" --output-format json
reasonix run "运行测试" --output-format stream-json
```

最终结构化对象的格式如下：

```json
{
  "type": "result",
  "subtype": "success",
  "is_error": false,
  "duration_ms": 123,
  "num_turns": 1,
  "result": "...",
  "session_id": "...",
  "total_cost_usd": 0,
  "usage": {
    "input_tokens": 0,
    "output_tokens": 0,
    "cache_read_input_tokens": 0,
    "cache_creation_input_tokens": 0
  }
}
```

执行失败时使用 `subtype: "error_during_execution"` 和 `is_error: true`。
结构化模式会把运行时错误保留在 JSON 中，不再额外重复输出一份人类可读错误。

### 脱敏机器接口

自动化只需要生命周期遥测、不能接收 prompt、reasoning、工具参数/输出或审批文本时，
使用独立的事件参数：

```sh
reasonix run --events-jsonl "运行 focused tests"
```

每行都包含 `schema_version`、`sequence` 和 `kind`，最后一行为
`kind: "run_done"`。`--events-jsonl` 与包含更多内容的
`--output-format stream-json` 是两个独立契约，不能和 `--output-format`
组合使用。

以下只读命令可以查询持久化状态，但不会输出 transcript、label、command、output、路径、
PID 或 hostname。这里的“只读”是指不会修改 transcript、runtime、recovery 或被查询的
状态；首次使用脱敏机器接口时，Reasonix 可能会在用户状态目录初始化一个私有身份密钥：

```sh
reasonix session list --json [--dir SESSION_DIR]
reasonix session show <machine-session-id> --json [--dir SESSION_DIR]
reasonix session status <machine-session-id> --json [--dir SESSION_DIR]
reasonix session recovery [<machine-session-id>] --json [--dir SESSION_DIR]
reasonix task list --json [--dir SESSION_DIR] [--session MACHINE_SESSION_ID]
reasonix task show <task-id> --json [--dir SESSION_DIR] [--session MACHINE_SESSION_ID]
reasonix hook list --json [--project-root PATH] [--home-dir PATH]
reasonix hook status --json [--project-root PATH] [--home-dir PATH]
```

对于 `session` 和 `task`，`--dir` 明确指定 session 存储目录；未指定时，Reasonix
选择当前项目的 session store。对于 `hook`，`--dir` 是 `--project-root` 的别名。
`hook list` 的状态值为 `active` 或 `invalid`；`invalid` 表示配置的
event 因事件名、命令/context 来源或工具事件 matcher 无效而无法执行。非工具事件
会忽略 matcher。

机器 session ID 是带密钥的 opaque hash，不是 transcript 文件名。在同一个 Reasonix
用户状态目录中，同一 session 的 ID 保持稳定；不同安装密钥会生成互不关联的 ID，无法再
根据时间戳或模型 label 离线猜测。迁移 Reasonix 状态目录时，如果自动化依赖已有 machine
ID，需要一并保留该私有身份密钥。任务仍在运行时
`finished_at` 为空；只有任务已经结束并且持久化产物存在时，才会输出
`artifact_complete=true`。没有 live session lease 的 `running` 记录会显示为
`interrupted`；再次打开该 session 时也会自动修复持久化生命周期状态。

Schema version 1 的兼容规则：

- 消费端必须忽略未知字段；
- 同一 schema version 内不会删除字段或改变字段类型；
- 空集合编码为 `[]`；
- 参数错误退出码为 `2`，状态/查询错误退出码为 `1`；
- 机器命令错误是带稳定 `error.code` 的 JSON 对象。

## 恢复会话

```sh
reasonix --continue
reasonix --resume
reasonix --resume provider-config
reasonix --resume <session-id>
reasonix --resume provider-config --copy
```

- `--continue` 立即恢复最新保存的会话。
- 在交互式终端中，单独使用 `--resume` 会打开可搜索选择器。
- `--resume QUERY` 接受精确 session ID 或路径，也支持唯一匹配标题或预览内容的
  子串。没有匹配或匹配不唯一时会返回明确错误。
- 为保持兼容，仍接受 `--resume=true` 和 `--resume=false`。
- `--copy` 不修改原 transcript，而是在新的可写会话中继续。原会话已被另一个
  Reasonix 进程占用时可以使用它。

一次性运行可用 `reasonix run --resume PATH "任务"` 指定 session 文件路径。Session
lease 会阻止桌面端和 CLI 同时写入同一个 transcript。

## 权限

```sh
reasonix --permission-mode plan
reasonix --permission-mode acceptEdits
reasonix -p "运行指定测试" --allowed-tools "Bash(go test ./...)"
reasonix --allowed-tools "Bash(git *) Edit"
reasonix --allowed-tools "Bash(go test ./...)" --allowed-tools read_file
```

| 模式 | 行为 |
| --- | --- |
| `manual`、`ask` | 普通权限决策会弹出审批。 |
| `auto` | 自动批准普通 fallback 操作，同时保留显式 ask 和 deny 规则。 |
| `acceptEdits` | 允许文件编辑工具；不等同于完整 Auto 模式。 |
| `dontAsk` | 未预先允许的请求直接拒绝，不弹出审批。 |
| `plan` | 以只读 Plan 模式启动交互式会话。 |
| `bypassPermissions` | 跳过审批；等同于 YOLO。 |

`--allowed-tools` 是会话权限覆盖，不是 provider tool schema 过滤器。规则可以用逗号
或空格分隔，也可重复传入参数。配置中的 deny 规则始终优先于命令行 allow 规则。

在非交互运行（`reasonix run` / `-p`）下没有可应答的审批，各模式都以非阻塞方式解析：
`ask`、`manual`、`acceptEdits` 保留 run 自主性，放行普通审批决策；`auto` 仍自动批准
普通 fallback，但对命中显式 ask 规则的命令改为拒绝，而不是无人值守地执行；`dontAsk`
拒绝；`bypassPermissions` 执行一切，仅始终需要人工新鲜批准的工具（记忆、plan、沙箱
逃逸、受管配置写入）除外。

## 附加目录

```sh
reasonix --add-dir ../shared
reasonix -p "同时更新两个项目" \
  --add-dir ../frontend \
  --add-dir ../backend
```

相对路径从 workspace 根目录解析，并且必须是已存在的目录。Reasonix 会解析符号链接、
去重，并在当前会话中扩展文件写入工具和沙盒 Bash 的写入边界。这些目录只在运行时生效，
不会写入配置。

## 交互操作

`/model`、`/provider` 和 `/resume` 使用可搜索选择器。审批提示也使用相同的行选择
交互，同时保留原有单键快捷操作。

| 按键 | 操作 |
| --- | --- |
| `Up` / `Down`、`Ctrl+P` / `Ctrl+N` | 在选择器或审批行之间移动。 |
| `j` / `k` | 搜索词为空时移动；开始搜索后作为普通 `j` / `k` 字符输入。 |
| 输入文字 | 过滤可搜索选择器。 |
| `Enter` | 选择当前高亮项。 |
| `Esc` | 取消当前选择器或审批。 |
| `y` / `a` / `p` / `n`、数字键 | 执行对应的审批动作。 |
| `Shift+Tab` | 按 `Ask → Auto → Plan → Ask` 循环。 |
| `Ctrl+Y` | 独立切换 YOLO，不进入安全模式循环。 |

响应式底栏左侧显示当前交互状态；空间足够时，右侧显示模型、推理强度和工作模式。第二行按
可用性显示仓库与会话遥测，例如缓存命中率、上下文占用、压缩余量、后台任务和余额。
“就绪”表示输入框当前空闲；进入选择器、审批、图片粘贴、shell 模式等需要用户关注的状态
时，这个位置会切换。窄终端会移动或压缩完整信息组，不会从中间截断标签。可见标签和工作
模式值会跟随 `/language`。

使用 `/theme auto|light|dark` 选择终端背景模式，也可以从 `/theme` 列出的命名配色中选择
强调色。输入框上下边线、插入光标、选区、滚动条和底栏都会使用当前 CLI 主题。Transcript
导航、多行输入、rewind 和剪贴板操作见[快捷键](./GUIDE.zh-CN.md#快捷键)。

剪贴板操作按内容类型明确分开。本地 transcript 和输入框选区写入系统剪贴板，并且只有写入
成功后才提示完成；SSH 会回退到明确标记的 OSC 52 请求。文本粘贴继续走终端的
bracketed-paste 动作（macOS 通常为 `Cmd+V`，其它平台使用终端自身配置）。Reasonix 接管
本地会话的鼠标时，没有选区的右键会读取剪贴板文本并走同一粘贴路径，有选区时右键优先复制。
SSH 下远端进程无法读取本机剪贴板，请使用终端粘贴快捷键；`/mouse` 可恢复终端原生右键菜单。
图片粘贴由 Reasonix 接管：macOS/Linux 使用 `Ctrl+V`，Windows 使用 `Alt+V`，也可运行
`/paste-image`；附件标记准备完成前，底栏会显示“正在粘贴图片…”。

## 会话内命令

在交互式会话中输入 `/help` 可查看完整命令列表。斜杠补全、帮助、dispatch 和别名来自
同一份 registry，因此界面展示与 TUI 实际接受的命令保持一致。

| 命令 | 用途 |
| --- | --- |
| `/model` | 搜索已配置模型并切换当前模型。 |
| `/provider` | 选择 provider，再选择该 provider 下的模型。 |
| `/resume` | 搜索最近会话并切换。 |
| `/status` | 显示模型、effort、cache、Git、后台任务，以及工作模式或余额信息。 |
| `/work-mode [economy\|balanced\|delivery]` | 查看或切换运行时工作模式；`/profile` 是别名。 |
| `/theme [auto\|light\|dark\|style]` | 查看或切换 CLI 背景模式和强调色。 |
| `/paste-image` | 读取剪贴板图片并插入可编辑的附件标记。 |
| `/mouse` | 切换应用内鼠标选区、滚动条和滚轮处理。 |
| `/effort` | 查看或切换 reasoning effort。 |
| `/output-style` | 选择回答风格。 |
| `/verbose` | 切换详细 reasoning 显示。 |
| `/sandbox` | 查看沙盒状态。 |
| `/goal` | 启动、查看或清除长周期 Goal。 |
| `/mcp`、`/skills`、`/hooks`、`/memory` | 查看和管理扩展或记忆。 |
| `/rewind` | 把对话和/或代码恢复到更早的 turn。 |
| `/tree`、`/branch`、`/switch` | 查看或切换会话分支。 |

切换模型、effort 或工作模式会重建运行时，同时保留当前对话、会话级权限覆盖、附加目录
访问权限和 session ownership。
