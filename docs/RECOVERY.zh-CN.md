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
reasonix-guard apply-plan --file PLAN.json [--preview-id ID] [--yes] [--allow-project]
reasonix doctor repair [--root PATH] [--apply] [--project] [--json]
```

Windows/Linux 安装后的桌面快捷方式默认先启动 Guard。macOS 应用 Bundle 则直接
启动 Wails 桌面进程，让 LaunchServices、Dock 图标和应用窗口拥有同一个原生进程身份；
它会在创建 WebView 前执行同等的启动恢复预检。Guard 仍作为独立恢复命令随包提供。
直接运行 `reasonix-guard` 会启动同目录的桌面程序；只读检查请显式使用 `check`。
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
Guard（macOS 上为桌面启动预检）会显示不依赖 WebView 的系统原生恢复对话框。
安全模式使用内置配置、不恢复上次标签页，并在本次运行中禁用外部集成；它不会改写
用户配置。

## 更新回滚

自动 **便携版** 更新前（Windows 安装器路径、Linux `.tar.gz`、或 macOS 应用 Bundle
替换），Reasonix 会完整保留安装的发布单元——桌面可执行文件以及安装器同样会替换的
Guard/启动器二进制，或整个应用 Bundle（macOS）。只有新版本进入 `healthy` 或干净
退出后才清理备份。新版本达到启动失败阈值时，Guard（macOS 上为桌面启动预检）会先
校验全部备份哈希，再恢复完整发布单元并重新启动，回滚不会留下新旧混装。Windows
安装器在桌面退出后执行失败时，更新 helper 会记录失败并重新拉起 Guard，Guard 启动时
立即执行同样的完整回滚，而不是等待崩溃循环。更新元数据和哈希位于 Reasonix 修复状态
目录；任何任意目标路径、目录外备份或缺失哈希的备份都会被拒绝。

**Debian/Ubuntu `.deb` 安装** 的升级不走 Guard 文件级回滚。应用内更新通过 Polkit
授权 root helper，并以 `apt-get --only-upgrade` 安装。失败时旧进程保持运行、已验证
的下载缓存可重试，包状态交由 apt/dpkg 处理。安装成功后由系统包管理器管理，Reasonix
不会自动降级。尚未包含 helper 的旧 `.deb` 用户需手动覆盖安装一次 bootstrap 包：
`sudo apt install ./Reasonix-linux-amd64.deb`（无需卸载）。

## 可选 AI 辅助

离线 `check`、`repair`、`diagnose`、版本回滚和安全模式都不会调用模型。
`assist` 是独立且必须显式触发的第二层：它把脱敏诊断摘要作为一次性请求发给用户选择
的已配置 Provider，可能产生 token 费用，但不会改变普通聊天的 system prompt、工具
列表或缓存前缀。

模型只能返回带版本号的 `RepairPlan` JSON。未知字段和非白名单动作会被拒绝。白名单
仅包括：隔离配置、恢复已校验快照、重建派生状态、回滚待确认更新。Host 会先展示操作
预览和配置统一 diff，再要求用户确认。计划不能运行 shell、修改凭据或会话正文，也不能
指定任意文件路径。

机器可读的 `assist` 输出包含 `planId` 和 `previewId`。Host 在 dry-run 后延迟确认时，
必须把同一个 `previewId` 传给 `apply-plan`；Guard 会在写入前重新计算预览，并在计划、
动作列表、文件 diff、派生状态输入或待回滚更新事务发生变化时拒绝执行。非交互式
`apply-plan --yes` 必须同时提供 `--preview-id`；交互式确认则自动绑定同一次调用中展示
的预览。自行管理确认边界的 Go 包调用方保持源码兼容，并可通过
`ApplyPlanOptions.ExpectedPreviewID` 启用相同校验。

已确认的配置、快照和派生状态变更会在 Reasonix 进程间串行化。修复事务记录与撤销会先于
各目标锁串行化，因此不同目标的并发修复不会交换或丢失撤销记录。Guard 获取目标锁后会
重新校验已确认的动作及其输入，并核对最终重命名实际移走的节点。如果其他进程在修复期间
重新创建目标文件，该进程的写入优先：Guard 不会覆盖新文件，并会报告已确认状态被移到的
隔离位置。待确认更新的回滚绑定完整事务身份，而不只比较版本号或时间戳。文件更新保持
`pending-update.json` 不可变，并把完整的已安装 release unit 写入当前事务专属、仅创建一次
的 sidecar；因此进程崩溃不会在两个状态文件替换之间隐藏回滚事务。

新增字段和 sidecar 都是增量数据：旧版 Reasonix 会忽略未知数据，当前版本也仍能读取缺少
新绑定的旧事务。缺失绑定只表示安全零值，不构成破坏性操作授权；无法证明节点归属时，
自动 handoff、健康提交或 app-bundle 回滚都会 fail closed。旧 app-bundle 备份没有目录树
摘要时，只有显式确认且绑定该备份的修复计划预览才能执行恢复。
