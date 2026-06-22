# 配置路径

从 **Reasonix v1.8.1** 开始，Reasonix 使用一个用户可见的全局目录存放配置和用户状态。CLI 与桌面端共用这个目录。

## Reasonix Home

| 平台 | Reasonix home |
| --- | --- |
| macOS | `~/.reasonix` |
| Linux | `~/.reasonix` |
| Windows | `%APPDATA%\reasonix` |

可以设置 `REASONIX_HOME` 覆盖 Reasonix home，主要用于测试、CI 或便携安装。普通用户通常不需要设置。

## 目录内容

| 数据 | 路径 |
| --- | --- |
| 全局配置 | `<Reasonix home>/config.toml` |
| 全局 credentials `.env` | `<Reasonix home>/.env` |
| 全局斜杠命令 | `<Reasonix home>/commands/` |
| 全局 skills | `<Reasonix home>/skills/` |
| 全局 hooks | `<Reasonix home>/settings.json` |
| hooks 信任状态 | `<Reasonix home>/trust.json` |
| 会话 | `<Reasonix home>/sessions/` |
| 归档 | `<Reasonix home>/archive/` |
| 记忆 | `<Reasonix home>/memory/` 与 `<Reasonix home>/projects/` |

setup 向导、桌面端或 CLI 缺 key 提示保存的新 API key 都会写入
`<Reasonix home>/.env`。这个全局 `.env` 是 provider credentials 的唯一运行时来源：
项目 `.env`、home `.env`、继承的 shell 环境变量和 Windows 环境变量都不会覆盖
Reasonix 保存的 key。`credentials_store` 已弃用，仅作为旧配置字段保留。

缓存仍放在系统缓存目录，例如 macOS 的 `~/Library/Caches/reasonix`、
Linux 的 `$XDG_CACHE_HOME/reasonix` 或 `~/.cache/reasonix`、Windows 的
`%LOCALAPPDATA%\reasonix\cache`。可以设置 `REASONIX_CACHE_HOME` 覆盖缓存根目录。

## 配置优先级

运行时配置按下面顺序解析：

```text
命令行参数
> 项目 ./reasonix.toml
> 全局 <Reasonix home>/config.toml
> 兼容读取的旧全局配置
> 内置默认值
```

写配置时始终写入新的全局路径：

```text
macOS/Linux: ~/.reasonix/config.toml
Windows:     %APPDATA%\reasonix\config.toml
```

## 旧路径迁移

从 **v1.8.1** 开始，Reasonix 启动时会在第一次加载配置前自动检查旧路径。迁移是同步、一次性、非破坏性的：旧文件会被复制或转换到 Reasonix home，原文件保留。

旧配置来源包括：

```text
~/Library/Application Support/reasonix/config.toml
~/.config/reasonix/config.toml
~/.reasonix/reasonix.toml
~/.reasonix/config.json
```

旧 credentials 文件或旧 keyring 里的密钥会在新目标尚未包含该 key 时导入到
`<Reasonix home>/.env`。旧 memory 文件和 sessions 会在新目标不存在时导入到
Reasonix home。若新的全局配置已经存在，则新配置优先；旧配置只作为兼容 fallback 保留。

从 **v1.9.1** 开始，Reasonix 还会在升级时把已知旧路径、legacy `config.json`、
桌面端已登记项目和恢复 tabs 对应项目里的 MCP 配置汇总补齐到全局
`<Reasonix home>/config.toml`。已有的全局 `[[plugins]]` 按名称优先，不会被旧
配置或项目配置覆盖；源文件会保留不变。该补齐会写入一次性 marker，避免用户之后
主动删除某个全局 MCP 时又被旧项目配置反复恢复。

## 手动补救迁移

如果 Reasonix 已经创建了新的 home 目录，但当时旧数据还不在可扫描路径里；或者先打开了桌面端，导致自动迁移没有把旧路径数据补齐，可以在任一前端运行补救命令：

```text
/migrate
```

在 CLI TUI 中，把 `/migrate` 输入到聊天输入框。在桌面端中，把同一个命令输入到 composer。命令会显示进度提示：

1. 检查旧配置和 credentials；
2. 扫描已知旧 memory 位置；
3. 扫描已知旧 sessions 目录；
4. 导入尚未迁移过的 memory 文件和 sessions；
5. 输出最终汇总。

该补救命令仍然是非破坏性的。它不会覆盖已有的
`<Reasonix home>/config.toml`；如果新配置已经存在，需要手动把旧配置里缺失的设置复制过去。旧 memory 文件只会在目标文件不存在时复制。它也会尊重 session 导入 marker，因此已经迁移过、之后又被用户删除的会话，不会在后续 `/migrate` 中被重新恢复。

版本限制：

- 自动迁移从 **v1.8.1** 开始。
- `/migrate` 只存在于包含该命令的 Go 版 Reasonix 构建中。如果 Reasonix 提示 `unknown command`，请先升级后再运行。
- legacy `0.x` TypeScript 线没有这个命令。
- 它只会重新扫描上面列出的旧路径；它不是备份恢复工具、降级导入工具，也不是任意目录导入器。
