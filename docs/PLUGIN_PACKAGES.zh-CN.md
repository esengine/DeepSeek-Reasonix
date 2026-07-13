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
Reasonix 尚未映射的 Claude 插件能力（`agents/`、`hooks/hooks.json`、
`.mcp.json`）会以安装警告的形式提示，而不是被静默丢弃。GitHub 仓库若在
`.claude-plugin/marketplace.json` 中通过 `./plugins/example` 或
`plugins/example` 这类相对字符串列出多个插件，可以直接从仓库根目录安装；
预检会在写入前逐项展示安装动作。填写可选安装名称时，可只选择 marketplace
中的同名插件。外部/object 来源、npm、`strict: false` 以及其他高级
marketplace 来源协议暂未实现：整库安装时这类条目会跳过并给出警告，
按名称选中这类条目时则直接报错。
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
- 如果存在 `hooks/session-start-codex`，映射为 Reasonix `SessionStart` hook。
- 插件根目录的 `CLAUDE.md` 会映射为内置的 `SessionStart` 上下文 hook。
  Reasonix 会直接读取该文件，不通过 shell 命令。
- `.claude/settings.json` 里的 command hooks 会按同名事件映射到 Reasonix hooks。
  Claude 的 `matcher` 字段会映射到 Reasonix `match`；hook 命令以插件根目录作为
  `cwd` 执行；Claude `timeout` 按秒解析。

不支持的 Claude hook item type 会跳过并产生 warning。Reasonix 不会执行第三方安装脚本。

插件 hook 会收到这些环境变量：

- `REASONIX_PLUGIN_ROOT`
- `REASONIX_PLUGIN_NAME`
- `REASONIX_PLUGIN_VERSION`
- `REASONIX_HOME`
- `REASONIX_WORKSPACE_ROOT`
- `CLAUDE_PROJECT_DIR`

## 桌面端后端方法

Desktop 通过 Wails 方法暴露插件包操作：

- `Plugins`
- `PlanPluginInstall`
- `InstallPlugin`
- `RemovePlugin`
- `SetPluginEnabled`
- `UpdatePlugin`
- `PluginDoctor`
