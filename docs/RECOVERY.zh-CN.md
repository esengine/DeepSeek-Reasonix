# 恢复与安全模式

Reasonix 随桌面端提供一个很小的恢复程序 `reasonix-guard`。它不加载 Wails、
WebView、插件、MCP、Hooks、机器人或会话正文，因此桌面壳层或 TOML 配置无法启动时，
它仍可使用。

## 命令

```bash
reasonix-guard check [--root PATH] [--json]
reasonix-guard repair [--root PATH] [--project] [--json]
reasonix-guard diagnose [--root PATH] [--network] [--json]
reasonix-guard rebuild --target tabs|projects|window|zoom|all
reasonix-guard snapshots [--json]
reasonix-guard restore --snapshot ID
reasonix-guard undo [--json]
reasonix-guard launch [--app PATH] [--safe-mode] [--detach]
reasonix-guard recover [--root PATH] [--project]
reasonix-guard assist [--model PROVIDER/MODEL] [--apply] [--allow-project]
reasonix-guard apply-plan --file PLAN.json [--yes] [--allow-project]
reasonix doctor repair [--root PATH] [--apply] [--project] [--json]
```

安装后的桌面快捷方式和应用 Bundle 默认先启动 Guard。因此直接运行
`reasonix-guard` 会启动同目录的桌面程序；只读检查请显式使用 `check`。
Windows 安装包的快捷方式使用同一套 Guard 代码编译出的无终端窗口启动器，同时保留
`reasonix-guard.exe` 供命令行诊断使用。
Windows/Linux 快捷方式启动桌面后会退出启动器；终端中显式执行
`reasonix-guard launch` 默认等待桌面退出，只有传入 `--detach` 才分离。

除非使用 `repair` 或 `--apply`，否则检查过程只读。执行修复时，无法解析的 TOML
会重命名为带时间戳的 `.reasonix-quarantine-*` 文件；全局配置损坏时，还会尝试恢复
桌面端成功启动后保存的最近健康快照。守卫不会删除凭据 `.env`、会话 JSONL 或项目
源码。只有显式传入 `--project` 时，项目的 `reasonix.toml` 才会被隔离。

Guard 保留最近 5 个健康的全局配置快照。每个快照都有 SHA-256，恢复前必须同时通过
哈希和 TOML 校验。每次配置或派生状态修复都会把可撤销状态持久化到
`last-repair.json`，并尽力追加到 `repair-log.jsonl`；审计日志写入失败不会让已经可靠
落盘的修复失效。`undo` 会恢复最近一次修复前被移走的文件，同时把当前修复版本保留成
可再次使用的副本。多动作的 `apply-plan` 记录为一个事务：一次 `undo` 即可回退整份
计划（计划中途失败时回退已执行的前缀）；被中断的 `undo` 重跑时会从断点继续。

`diagnose` 增加离线语义检查：模型引用、Provider/MCP URL、凭据、代理结构、MCP
命令、权限规则冲突、文件权限和桌面派生 JSON。只有显式传入 `--network` 才会按当前
代理探测 Provider 模型接口；结果只记录连通性和认证状态，不保存响应正文。
`rebuild` 不删除派生数据，而是先隔离指定文件，再让 Reasonix 自动重建。

## 自动安全模式

桌面端会在 Reasonix 状态目录记录 `starting`、`ready`、`healthy` 和
`clean-exit`。`ready` 后还有 30 秒稳定观察期。五分钟内连续三次未完成启动时，
Guard 会显示不依赖 WebView 的系统原生恢复对话框。安全模式使用内置配置、不恢复
上次标签页，并在本次运行中禁用外部集成；它不会改写用户配置。

## 更新回滚

自动更新前，Reasonix 会完整保留安装的发布单元——桌面可执行文件以及安装器同样会
替换的 Guard/启动器二进制（Windows/Linux），或整个应用 Bundle（macOS）。只有新
版本进入 `healthy` 或干净退出后才清理备份。新版本达到启动失败阈值时，Guard 会先
校验全部备份哈希，再把记录的所有二进制一起恢复后启动，回滚不会留下新旧混装。
Windows 安装器在桌面退出后执行失败时，更新 helper 会记录失败并重新拉起 Guard，
Guard 启动时立即执行同样的完整回滚，而不是等待崩溃循环。更新元数据和哈希位于
Reasonix 修复状态目录；任何任意目标路径、目录外备份或缺失哈希的备份都会被拒绝。

## 可选 AI 辅助

离线 `check`、`repair`、`diagnose`、版本回滚和安全模式都不会调用模型。
`assist` 是独立且必须显式触发的第二层：它把脱敏诊断摘要作为一次性请求发给用户选择
的已配置 Provider，可能产生 token 费用，但不会改变普通聊天的 system prompt、工具
列表或缓存前缀。

模型只能返回带版本号的 `RepairPlan` JSON。未知字段和非白名单动作会被拒绝。白名单
仅包括：隔离配置、恢复已校验快照、重建派生状态、回滚待确认更新。Host 会先展示操作
预览和配置统一 diff，再要求用户确认。计划不能运行 shell、修改凭据或会话正文，也不能
指定任意文件路径。

所有新增状态文件都是可选且向后兼容的；旧版 Reasonix 会直接忽略，缺失新字段时按安全
零值处理。
