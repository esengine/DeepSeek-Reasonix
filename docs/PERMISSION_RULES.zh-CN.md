# 权限规则

`reasonix.toml`（项目级）或 `config.toml`（用户级）中的 `[permissions]` 一节，决定 Reasonix 对每一次工具调用是直接执行、先征求你的同意，还是直接拒绝。本文档说明精确的规则语法，让权限规则不再靠猜。

关于桌面端“询问 / 自动 / Yolo”控制与已配置策略的关系，见 [工具权限：询问、自动与 Yolo 模式](./TOOL_APPROVAL_MODES.zh-CN.md)。

## 配置结构

```toml
[permissions]
mode  = "ask"        # ask | allow | deny —— 写操作工具的回退决策
allow = ["Edit(src/**)", "Bash(go test:*)"]
ask   = ["Bash(git push:*)"]
deny  = ["Bash(rm -rf*)"]
```

- `mode` 是当没有规则命中时，写操作工具的兜底决策（默认 `ask`）。只读工具永远回退为 **allow**，不采用 `mode`。
- `allow`、`ask`、`deny` 是规则列表。一次调用的最终决策按严格的优先级解析：

```
deny  > ask  > allow  > 回退
```

  也就是说，即使某条 allow 规则同时命中，只要命中 deny 规则，调用就会被阻止。

## 规则形式

| 形式 | 含义 |
| --- | --- |
| `ToolName` | 匹配该工具的所有调用。 |
| `ToolName(specifier)` | 匹配 subject 与 specifier（glob 或 bash 前缀）匹配的调用。 |
| `ToolName=literal` | 遗留精确匹配形式：按字面逐字匹配 subject，不做 glob 展开。保留它以便旧配置继续生效。 |

规则前后的空白会被去除。工具名为空的规则无效。

### 工具名与工具 ID

规则用友好名称 `Bash` 指代 `bash` 工具，用 `Edit` 指代**文件变更组**——`write_file`、`edit_file`、`multi_edit`、`move_file`、`notebook_edit`、`delete_range`、`delete_symbol`。因此一条裸的 `Edit` 规则一次覆盖全部七个文件写工具。

其它名字都是字面的工具 ID，包括其余内建工具（`read_file`、`grep`、`glob`、`ls`、`bash_output`、`wait` 等）、MCP 服务工具以及会话工具。工具 ID 为小写，与 [TOOL_CONTRACT](./TOOL_CONTRACT.zh-CN.md) 中的 `Tool` 列一致。

### specifier 语法

specifier 与调用的 **subject** 匹配——也就是标识该工具操作对象的字符串：

- `bash` → 命令行。
- 文件工具（`Edit` 组、`read_file`）→ 文件路径。
- `move_file` → 同时匹配 `source_path` 与 `destination_path`；只有每个路径都通过时调用才会被放行。
- `grep` / `glob` → 搜索模式。
- `ls` → 目录路径。

glob 使用两种通配符：

- `*` 匹配任意一串字符，**包括 `/` 与 `\`**——因此 `src/**` 能匹配 `src/` 下任意深度的文件。
- `?` 匹配恰好一个字符。

模式必须匹配完整 subject（不是子串搜索）。这里不支持 shell 方括号表达式。

**Bash 前缀规则。** 以 `:*` 结尾的 specifier（例如 `Bash(go test:*)`）是命令前缀规则：它匹配前几个词与前缀在词边界处一致的所有命令，因此 `go test ./...` 和 `go test -v ./...` 都能命中 `Bash(go test:*)`。复合命令按段分别匹配，因此 `git add . && git commit && git push` 会被 `Bash(git push:*)` 覆盖。遗留的 `Bash(cmd *)`（空格-星号）形式同样被接受。

**绝对路径与相对路径。** specifier 匹配的是模型传入的原始路径字符串。相对工作区的 glob（如 `Edit(src/**)`）是更稳妥的选择。绝对路径（`Edit(/etc/*)`、`Edit(C:\...)`）以及 `..` 越界路径会被接受，但会给出警告——因为越过工作区的规则通常会破坏“只放行工作区内、锁住工作区外”的意图。

### 校验行为

结构性问题会在**保存规则时被拒绝**（设置 → 权限规则，或通过配置写入）：无法解析的规则、空 specifier（`Edit()`、`Bash=`）、括号不平衡的 specifier（`Edit(src`）。

警告则在**诊断**中给出——`reasonix doctor capabilities`，或桌面端“设置 → 诊断”：

- 工具名不是内建工具（MCP 与会话工具名合法，因此这只是一次拼写检查，不是错误）。
- specifier 是绝对路径或会越出工作区的路径。
- specifier 首尾带空白，永远无法匹配真实调用（`Edit(src/** )` 匹配的是字面路径 `src/** `）。
- 配置文件中已经存在的结构性问题。

诊断页还会列出每个内建工具在无参数调用下的有效决策——是哪条规则生效（`allow` / `ask` / `deny`），还是走回退——这样你可以确认规则确实起作用了。
