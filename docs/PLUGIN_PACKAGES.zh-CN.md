# Reasonix 插件包

Reasonix 插件包把 skills、hooks 和 MCP servers 组织成一个可安装单元。

## CLI 模式

在终端里使用 `reasonix plugin` 安装和管理插件包。插件包当前按全局范围安装，
写入 Reasonix home 目录。

### 通过 CLI 安装

`install` 接收一个来源：

- GitHub 仓库，例如 `git:github.com/obra/superpowers` 或
  `https://github.com/obra/superpowers`。
- GitHub 分支或子目录 URL，例如
  `https://github.com/owner/repo/tree/main/path/to/plugin`。
- 本地目录，目录内需要包含 `reasonix-plugin.json`、
  `.codex-plugin/plugin.json` 或 `.claude-plugin/plugin.json`。

只预览安装计划，不写文件：

```bash
reasonix plugin install git:github.com/obra/superpowers --dry-run
```

确认计划后安装：

```bash
reasonix plugin install git:github.com/obra/superpowers --yes
```

指定安装名称，或覆盖已安装的同名插件：

```bash
reasonix plugin install git:github.com/obra/superpowers --name superpowers --replace --yes
```

以开发模式使用本地目录：

```bash
reasonix plugin install /path/to/plugin --link --replace --yes
```

CLI 安装参数：

- `--dry-run` 只规划和校验安装，不写文件。
- `--yes` 用于确认执行会写文件的安装。
- `--replace` 允许当前来源替换已安装的同名插件。
- `--name <name>` 或 `--name=<name>` 覆盖插件 manifest 里的名称，
  作为本次安装名称。
- `--link` 链接本地插件目录，而不是复制到 Reasonix 的插件存储目录。
  移动或删除该目录会导致这个链接插件失效。

如果运行 `reasonix plugin install <source>` 时既没有 `--dry-run`，
也没有 `--yes`，CLI 会拒绝写文件，并提示使用其中一个参数重新运行。
安装和移除命令会输出结构化 JSON，来源于桌面端同一套 install-source 后端。

插件状态和内容写入：

```text
~/.reasonix/plugin-packages.json
~/.reasonix/plugins/<name>/
```

### 通过 CLI 管理

列出已安装插件：

```bash
reasonix plugin list
```

查看某个插件的元数据、根目录、来源以及导出的能力数量：

```bash
reasonix plugin show superpowers
```

如果能读取到能力明细，`show` 也会输出具体清单：

- **skills** 会展示建议的 `/<插件名>:<技能名>` 调用方式和描述。
- **commands** 会展示 `/<插件名>:<命令名>` 调用方式、参数提示和描述。
- **hooks** 会展示生命周期事件、matcher、命令或上下文文件。
- **mcpServers** 会展示服务器名称、传输方式和启动目标。

检查 manifest 和 skill roots 是否可读：

```bash
reasonix plugin doctor superpowers
```

工作区级能力总览（skills / hooks / MCP 合并 / 包根目录）见
[能力诊断](./CAPABILITY_DIAGNOSTICS.zh-CN.md)：

```bash
reasonix doctor capabilities --json
# 桌面端：设置 → 诊断
# Agent：  /reasonix-guide
```

在不卸载的情况下启用或禁用插件：

```bash
reasonix plugin disable superpowers
reasonix plugin enable superpowers
```

移除插件：

```bash
reasonix plugin remove superpowers --yes
```

`remove` 也可以写成 `uninstall`。它需要 `--yes`，
因为会写入状态并删除复制安装的插件内容。如果是链接模式安装的本地插件，
外部源目录会保留。

### 在 CLI 中使用已安装插件

已安装插件不会打开一个独立聊天界面。插件启用后，Reasonix 会把它的能力加载到普通交互会话里：

- 在交互会话里运行 `/plugins` 可以列出已安装插件包。
  运行 `/plugins show <name>` 可以在不离开聊天的情况下查看该插件导出的
  skills、hooks、MCP servers 和使用提示。
- **Skills** 会出现在 `/skills` 中。可以用 `/<插件名>:<技能名> [args]` 直接调用，
  也可以自然描述任务，让 agent 按 description 选择匹配的 skill。
- **Hooks** 会在配置的生命周期事件里自动运行，例如 `SessionStart`、
  `UserPromptSubmit`、`PreToolUse` 或 `PostToolUse`。
- **MCP servers** 会进入正常 MCP/工具流程。用户只需要描述任务，
  Reasonix 会在相关时调用插件提供的工具。

如果是在另一个终端里安装、启用、禁用或更新插件，而当前已有 `reasonix` 会话正在运行，
建议开启新会话，或重新打开 `/skills` 确认当前会话能看到预期技能。

## 桌面端设置

打开 **设置 -> 插件**，可以不用 CLI 直接安装和管理插件包。

### 安装插件

安装区有两种模式：

- **本地目录**：点击 **选择插件目录**，从磁盘选择一个插件目录。
  选中路径会显示在按钮右侧。
- **Git 仓库**：填写 Git 来源，例如 `git:github.com/obra/superpowers`。
  **安装名称（可选）** 可覆盖插件 manifest 声明的名称，用于本次安装或覆盖。

选择来源和选项后，再使用操作按钮：

- **预检** 校验来源并展示计划安装动作，不写入文件。
- **安装插件** 按当前来源和选项执行安装。
- **刷新插件** 从磁盘和配置重新读取已安装插件列表。

安装选项：

- **覆盖同名插件** 允许当前来源替换已安装的同名插件。关闭时，同名安装会失败，
  而不是覆盖已有内容。
- **开发模式：链接源目录** 只在 **本地目录** 模式出现。它不会复制插件，
  而是直接链接所选目录；适合开发或调试插件。移动或删除该目录会导致这个链接插件失效。

对新的 Git 来源或本地插件目录，建议先点 **预检**。

### 管理已安装插件

已安装插件列表会展示每个插件包以及它导出的 skills、hooks 和 MCP servers。
通过应用外编辑插件文件或配置后，可点 **刷新插件** 重新读取。

展开插件行后可以：

- 启用或禁用插件。
- 查看 **使用方法**，了解该插件导出的 skills、hooks 和 MCP servers。
- 使用 **更新** 拉取或刷新具备更新来源的插件。
- 使用 **诊断** 检查插件 manifest，并查看警告或诊断信息。
- 使用 **移除插件**，确认后卸载该插件包。

### 在桌面端使用已安装插件

桌面端设置页和 CLI 使用同一套运行模型：

- 展开已安装插件，可以看到 **使用方法** 区域。
- 在任意桌面会话里输入 `/plugins` 可以列出已安装插件；
  输入 `/plugins show <name>` 可以直接从聊天界面查看同一套使用详情。
- Skills 会展示带插件名的直接命令，例如 `/superpowers:writing-plans`；
  在会话中也可以通过 `/skills` 浏览。
- 插件命令统一以带插件名的形式展示和调用，例如 `/superpowers:plan`。
- Hooks 和 MCP servers 作为透明能力清单展示。它们不需要单独的“运行”按钮：
  启用的 hooks 会自动触发，MCP 工具会通过普通工具调用流程可用。
- 如果当前打开的会话没有反映插件变更，刷新插件列表并开启新会话。

## 原生 Manifest

Reasonix 原生插件在根目录声明 `reasonix-plugin.json`：

```json
{
  "name": "example",
  "version": "1.0.0",
  "description": "Example plugin",
  "skills": "skills",
  "hooks": {
    "SessionStart": [
      {
        "command": "hooks/session-start",
        "description": "Load startup context"
      }
    ]
  },
  "mcpServers": {
    "helper": {
      "command": "bin/helper"
    }
  }
}
```

相对路径都按插件根目录解析。Reasonix 安装插件时不会执行第三方安装脚本。

## Codex 与 Claude 兼容

Reasonix 也会读取 `.codex-plugin/plugin.json` 和 `.claude-plugin/plugin.json`。
安装预检会结构化显示“完全兼容 / 部分兼容 / 不兼容”、已映射能力和每个被跳过
的条目。非原生插件如果没有任何可映射能力，会直接阻止安装，不再留下“安装成功
但不可用”的记录。“完全兼容”指清单里声明的每个能力都成功解析并映射到了
Reasonix 的对应实现，并不代表导入 Hook 的每一种运行时决策都被遵守。
`PreToolUse`/`PermissionRequest` 的“拒绝”与 `PermissionRequest` 的“批准”已经
实现；但 Hook 的 `updatedInput`，以及 `PreToolUse` 的 `ask`/`defer` 决策，是
脚本在实际运行时通过 stdout 决定的，并非清单里的静态字段，因此安装阶段无法
据此标记——具体已实现范围见下面 Hook 条目。GitHub 仓库若在
`.claude-plugin/marketplace.json` 中通过 `./plugins/example` 或
`plugins/example` 这类相对字符串列出多个插件，可以直接从仓库根目录安装；
预检会在写入前逐项展示安装动作。填写可选安装名称时，可只选择 marketplace
中的同名插件。对象来源仅接受 GitHub 仓库 URL 加完整 commit SHA；未固定版本的
外部字符串、npm、`strict: false` 以及其他高级 marketplace 协议在整库安装时会
跳过，按名称选中时则直接报错。
对于 Superpowers 和 Claude 风格 skill 包，Reasonix 会映射：

- `skills` 到 Reasonix skill root。Claude 清单若未声明 `skills` 字段，会回退到
  约定目录 `skills/`（或 `.claude/skills/`），与 Claude 自身的自动发现一致。
  插件 skill 统一以 `/<插件名>:<技能名>` 展示和调用。无歧义的 `/<技能名>`
  仍作为隐藏兼容别名接受输入；项目和用户 skill 保留短名称，多个插件导出的
  同名 skill 则只能通过各自的限定名称独立调用。这一用户侧命名空间不会改变
  模型 skill 索引或 `run_skill` 工具使用的内部短标识。
- `commands/`（以及 `.claude/commands/`）映射为 Reasonix 自定义斜杠命令：每个
  `<name>.md` 提示词模板统一以 `/<插件名>:<命令名>` 展示和调用，frontmatter 的
  `description` / `argument-hint` 以及 `$ARGUMENTS` / `$1..$N` 替换均生效。
  当短名称没有歧义时，`/<命令名>` 仍作为隐藏兼容别名接受输入，但不会出现在
  补全、帮助、桌面菜单、ACP 命令发现或提供给模型的命令清单中。用户和项目命令
  始终占有自己的短名称；多个插件导出同名命令时不会生成短名称别名。显式自定义
  命令也可以占用限定名称，Desktop 插件详情会报告该冲突。原生
  `reasonix-plugin.json` 清单也可以通过 `"commands"` 路径列表显式声明。
- `agents/*.md` 映射为插件所属、需要手动调用的子代理配置。Claude 模型别名会继承
  当前 Reasonix 模型；内联 `tools` 列表会转换为 Reasonix 工具名，并支持
  `mcp__*__search` 这类 MCP 通配符。Agent 使用独立的
  `/<插件>:agent:<名称>` 命名空间，因此上游 Agent 与 Skill 同名时不会互相遮蔽。
- 如果存在 `hooks/session-start-codex`，映射为 Reasonix `SessionStart` hook。
- 插件根目录的 `CLAUDE.md` 会映射为内置的 `SessionStart` 上下文 hook。
  Reasonix 会直接读取该文件，不通过 shell 命令。
- `.claude/settings.json` 和 `hooks/hooks.json` 里的 command hooks 会按同名事件映射。
  `matcher`、`args`、`async`、`env` 和 timeout 均会保留。`matcher` 以及 Hook 脚本看到的
  `tool_name` 会在 Reasonix 与 Claude 的工具名之间互译（`bash` ↔ `Bash`、
  `write_file` ↔ `Write` 等），因此 `"Bash"` 这类 matcher 能正确触发；Reasonix 里所有会
  启动子代理的工具（`task`、`read_only_task`、`parallel_tasks`，以及专用的
  `explore`/`research`/`review`/`security_review` 包装工具）都会映射到 Claude 唯一的
  `Agent` 工具，matcher 里旧名 `Task` 依然可用。`tool_input` 里字段名不同的键也会改名——
  每个映射后的 `Agent` 载荷都会包含 Claude 必填的 `prompt` 和 `description`；若
  Reasonix 调用省略了可选描述，会补一个稳定的操作标签。
  `Read`/`Write`/`Edit`/`MultiEdit` 的 `path` 改成 `file_path`，`NotebookEdit` 的
  `path` 改成 `notebook_path`，`Skill` 的 `name`/`arguments` 改成 `skill`/`args`，
  当前 `TaskOutput`/`TaskStop` 的 `job_id` 改成 `task_id`，专用子代理包装
  工具的 `task` 改成 `Agent` 的 `prompt`，`parallel_tasks` 则会把各子任务的 prompt
  合成为 `Agent` 的 `prompt`（原 `tasks` 数组保留）——这样读取
  `.tool_input.file_path` 或 `.tool_input.prompt` 的防护 Hook 才不会因为拿到空值
  而失败放行。旧的 `BashOutput`/`KillShell` matcher 仍能触发，但下发名称和字段使用
  Claude 当前词汇；`bash_output` 会补齐 `TaskOutput` 的非阻塞必填字段，`wait` 也会
  映射为 `TaskOutput`，单任务等待时包含 `task_id`，无限等待时省略可选的
  `timeout`，而不是谎报 0 毫秒预算。`AskUserQuestion` 会补省略的
  `multiSelect:false` 和空选项描述，`TodoWrite` 会用任务内容补省略的 `activeForm`；
  `NotebookEdit` 则会从 Reasonix 接受的别名补 `new_source`，删除或空单元格操作补空串。
  相对的 `file_path`/`notebook_path` 会按载荷 `cwd` 解析为绝对路径，
  与 Claude 文件工具契约一致，前缀匹配的防护 Hook 检查的就是工具实际访问的路径。
  `Bash` 的 `tool_response` 按 Claude 的 `{stdout, stderr, interrupted}` 形态下发
  （Reasonix 的合并输出放在 `stdout`，失败错误文本作为 `stderr`），官方
  security-guidance 插件的 commit/push 检查读取的正是这些字段；其他工具的结果仍按
  原样透传。导入 Hook 的
  stdin 使用 Claude 兼容的 snake_case 载荷（包括 `hook_event_name`）。宿主会在启动
  进程前展开 `${CLAUDE_PLUGIN_ROOT}` 和 `${REASONIX_PLUGIN_ROOT}`，也兼容不带花括号的
  `$NAME` 与 Windows `%NAME%` 写法，因此插件相对路径不再依赖目标 shell 的环境变量
  语法。Windows 上显式声明的裸 `sh -c`/`bash -c` hook 会复用 Git for Windows Bash
  探测，即使 Bash 不在 `cmd.exe` 的 `PATH` 中也能执行；带目录的显式解释器路径保持
  不变。如果机器确实没有可用 Bash，hook 会返回清晰的依赖提示，而不是本地化的
  “无法识别 sh”乱码。旧代码页输出也会在进入界面前转换为 UTF-8。`PreToolUse` 和
  `UserPromptSubmit` hook 仍可
  通过退出码 2 或退出码 0 时的 JSON 拒绝形态拒绝该次调用（`PreToolUse` 用
  `hookSpecificOutput.permissionDecision`，`UserPromptSubmit` 用顶层
  `decision:"block"`）；导入的 `PermissionRequest` hook 还能直接代答权限弹窗
  （拒绝或自动批准，而不只是发通知），通过退出码 2 或
  `hookSpecificOutput.decision.behavior` 实现，与 Claude 官方语义保持一致。
  `updatedInput` 暂未应用到实际工具调用参数；Hook 的 `if` 条件和 `asyncRewake`
  字段也不会被求值。声明其中之一、声明 `Stop`/`SubagentStop` hook（Reasonix 中
  不能阻止本轮结束），或 matcher 覆盖三种无法无损表达的输入时，插件都会报告
  部分兼容并附具体警告：`WebFetch.prompt`、Reasonix 以 `cell_number` 调用时的
  `NotebookEdit.cell_id`，以及 Reasonix `wait` 同时覆盖多个/全部任务时的
  `TaskOutput.task_id`。每类结构性缺口在每个 hooks 文件里只报告一次，
  通配 matcher 的插件每类缺口只会看到一条警告，而不是每个 hook 一条。
- 插件根目录 `.mcp.json` 会映射为已安装 MCP。Claude 的 `local` 会转换为 stdio；
  中文等显示名称会生成稳定内部 ID；重复声明会去重。导入服务器默认
  `auto_start=false`，由用户按需连接，避免启动时改变提供给模型的工具 schema。

不支持的 Claude hook item type 会跳过并产生 warning。Reasonix 不会执行第三方安装脚本。

插件 hook 会收到这些环境变量：

- `REASONIX_PLUGIN_ROOT`
- `REASONIX_PLUGIN_NAME`
- `REASONIX_PLUGIN_VERSION`
- `REASONIX_HOME`
- `REASONIX_WORKSPACE_ROOT`
- `CLAUDE_PROJECT_DIR`
- `CLAUDE_PLUGIN_ROOT`

## 桌面端后端方法

Desktop 通过 Wails 方法暴露插件包操作：

- `Plugins`
- `PlanPluginInstall`
- `InstallPlugin`
- `RemovePlugin`
- `SetPluginEnabled`
- `UpdatePlugin`
- `PluginDoctor`
