# 能力诊断

<a href="./CAPABILITY_DIAGNOSTICS.md">English</a>
&nbsp;·&nbsp;
<a href="./GUIDE.zh-CN.md">使用指南</a>
&nbsp;·&nbsp;
<a href="./PLUGIN_PACKAGES.zh-CN.md">插件包</a>

Reasonix 提供 CLI 与桌面端 **设置 → 诊断** 共用的只读能力诊断模型，覆盖 Skills、
Commands、Hooks、插件包、MCP 服务器，以及指令文件（`AGENTS.md` /
`REASONIX.md` / `CLAUDE.md`）。

**写入策略**

| 模式 | 配置文件 | MCP stats / schema cache | 网络 / MCP 进程 |
| --- | --- | --- | --- |
| 静态（默认）+ 桌面端 | 永不写入（`LoadForRootReadOnly`） | 永不写入 | 无 |
| CLI `--live` | 永不写入 | **不写入**（`SkipPersistence`） | 在隔离 Host 中启动 automatic MCP |

## 怎么用（快速上手）

| 目标 | 命令 / 入口 |
| --- | --- |
| 检查当前工作区的 skills / hooks / MCP / 插件 | `reasonix doctor capabilities` |
| 机器可读报告（CI / 报障） | `reasonix doctor capabilities --json` |
| 指定项目根目录 | `reasonix doctor capabilities --root /path/to/project` |
| 真实探测 MCP 启动（会启动第三方服务器） | `reasonix doctor capabilities --live --timeout 5s` |
| 让 Agent 按手册排障 | 会话中 `/reasonix-guide`，或自然语言描述症状 |
| GUI 健康视图 | 桌面端 **设置 → 诊断** |

**默认是静态且安全的**：无网络、不启动 MCP 子进程。只有你明确需要启动
automatic MCP 时才用 `--live`。

其它既有 doctor 命令（行为不变）：

```bash
reasonix doctor                  # 环境 / provider / 沙箱快照
reasonix doctor session <id>     # 支持用会话包
reasonix doctor redact-sessions  # 脱敏会话中的密钥
```

## 日常工作流

### 1. 「Skill / 命令找不到或内容不对」

```bash
reasonix doctor capabilities --json | jq '.skills.entries, .commands.entries, .issues'
```

关注：

- `skill.shadowed` / `command.shadowed` — 更高优先级路径覆盖了它
- `skill.disabled` — 名字在 `[skills].disabled_skills` 里
- `skill.missing_description` — 能加载但索引描述很弱
- `command.read_failed` — 文件读失败或解析失败

然后到 **设置 → 技能**，或直接改 `.reasonix/skills` / `.reasonix/commands` 下的文件。

### 2. 「项目 Hooks 不触发」

```bash
reasonix doctor capabilities | sed -n '/Hooks/,/Plugins/p'
```

若出现 `hook.untrusted_project`，在 **设置 → Hooks**（或 CLI trust 流程）信任该
工作区。`match` 是**锚定**正则：`file` **不会**匹配 `read_file`。

### 3. 「配置了 MCP 但模型看不到工具」

1. 先做静态检查（无副作用）：

   ```bash
   reasonix doctor capabilities --json | jq '.mcp.servers, .issues[] | select(.subsystem=="mcp")'
   ```

2. 仅在接受启动第三方服务器时：

   ```bash
   reasonix doctor capabilities --live --timeout 10s --json
   ```

常见 code：`mcp.command_not_found`、`mcp.invalid_transport`、
`mcp.start_failed`、`mcp.no_tools`。桌面端更推荐 **设置 → 诊断** 打开
「包含当前会话运行状态」——只读取**活动标签 Host**，不会再起第二个 Host。

### 4. 让 Agent 按手册排查（`reasonix-guide`）

交互式会话中：

```text
/reasonix-guide
```

或：

```text
我配置了 MCP 服务器 X，但模型始终看不到它的工具，请排查。
```

该内置 Skill 是 **inline**（`runAs: inline`）。它会优先要求模型运行：

```bash
reasonix doctor capabilities --json
```

只有你明确允许启动外部 MCP 时才建议 `--live`。项目或全局同名
`reasonix-guide` 会覆盖内置版；也可用
`[skills].disabled_skills = ["reasonix-guide"]` 隐藏。

## CLI 参考

```bash
reasonix doctor capabilities [--root PATH] [--json] [--live] [--timeout 5s]
```

| 参数 | 含义 |
| --- | --- |
| `--root` | 工作区根目录（默认当前目录），走 `config.LoadForRoot` |
| `--json` | 仅向 **stdout** 输出一个 JSON 对象（提示写 stderr） |
| `--live` | 在隔离 Host 中启动 **automatic** MCP（可能联网） |
| `--timeout` | 单服务器 live 超时，**1s–60s**，默认 `5s`，必须配合 `--live` |

### 模式

| 模式 | 行为 |
| --- | --- |
| **静态（默认）** | 无网络；不启动 stdio / HTTP / SSE MCP 子进程 |
| **Live（`--live`）** | stderr 风险提示；只探测 automatic 启动意图；`auto_start=false` → `skipped`；并发 4；始终关闭 Host |

桌面端「包含当前会话运行状态」**不等于** CLI `--live`：桌面只**读取**活动标签 Host，
不启动 MCP。

### 退出码

| 码 | 含义 |
| --- | --- |
| `0` | 无 `error` 级问题（warning/info 允许） |
| `1` | 存在 `error` 或 live MCP 启动失败 |
| `2` | 参数错误 |

示例：

```bash
# 当前目录、人类可读
reasonix doctor capabilities

# CI：仅有 error 时非零退出
reasonix doctor capabilities --json

# live 探测，超时 15 秒
reasonix doctor capabilities --live --timeout 15s --json 2>live-warn.txt
```

既有 `reasonix doctor` / `doctor session` / `doctor redact-sessions` 的 JSON
schema **不会**混入新字段。

## 桌面端

打开 **设置 → 诊断**：

| 控件 | 行为 |
| --- | --- |
| 打开页面 | 对活动工作区根加载**静态**报告 |
| 刷新 | 按当前「会话运行状态」开关重新收集 |
| 复制脱敏 JSON | 可安全粘贴的报告（路径已脱敏） |
| 包含当前会话运行状态 | 仅合并活动标签 Host 的 connected / failed / deferred / disabled |
| 前往设置（Issue 上） | 当 `settings_tab` 有值时跳到 MCP / Skills / Plugins / Hooks |

页面不提供自动编辑、自动 trust、自动启用或自动重连。打开诊断页**不会**
rebuild controller，也不会 snapshot 会话。

## JSON schema（version 1）

顶层字段：`schema_version`、`root`、`live`、`summary`、
`instructions` / `skills` / `commands` / `hooks` / `plugins` / `mcp`、`issues`。

Issue 含稳定 `code`、`severity`、`subsystem`、`source`、`message`、`remediation`、
可选 `settings_tab`。数组与 Issue 顺序确定，便于脚本与测试。

常见 code：

- `skill.shadowed`、`skill.missing_description`、`skill.disabled`
- `command.shadowed`、`command.read_failed`
- `hook.untrusted_project`、`hook.invalid_matcher`、`hook.missing_command`、`hook.malformed_settings`
- `plugin.missing_root`、`plugin.invalid_manifest`、`plugin.compatibility`
- `mcp.invalid_transport`、`mcp.command_not_found`、`mcp.missing_command`、`mcp.missing_url`
- `mcp.start_failed`、`mcp.no_tools`、`mcp.runtime_unavailable`

### 严重度

| 严重度 | 含义 | CLI |
| --- | --- | --- |
| `error` | 配置损坏或 live 启动失败 | 退出 `1` |
| `warning` | 需处理但非致命 | 退出 `0` |
| `info` | 遮蔽、禁用、无运行时等 | 退出 `0` |

## 路径与密钥安全

路径显示为 `<workspace>/...`、`~/...` 或 `<external>/basename`。
不输出用户名、完整外部路径、环境变量值、Header 值、token、URL query。
MCP 仅列出 env/header 的 **key**。可能携带 HTTP 响应体或 MCP stderr 的
错误文本会先经过全局密钥脱敏器（Authorization、Bearer/JWT/厂商 token、
`KEY=value` 与 JSON `"key":"value"` 凭据形态、Cookie/Set-Cookie 值），
再截断到 400 字符。向 issue / 聊天贴报告时，优先复制诊断 JSON，
不要贴原始配置文件。

## 不在本诊断范围内的事项

| 需求 | 改用 |
| --- | --- |
| Provider 密钥、代理、沙箱 OS 支持 | `reasonix doctor` |
| 给支持用的完整会话包 | `reasonix doctor session <id>` |
| 单个插件包 | `reasonix plugin doctor <name>` |
| 会话内 MCP 列表 | `/mcp` |

## 缓存影响

内置 `reasonix-guide` 仅在 system prompt 的 Skill 索引中增加 **一行稳定索引**；
正文按需加载。诊断本身不进入 provider 请求。
