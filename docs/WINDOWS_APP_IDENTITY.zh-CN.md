# Windows 应用身份

Reasonix Desktop 的 Electron 窗口、launcher、Go 桌面进程、快捷方式和通知统一使用
不随版本变化的 AppUserModelID `io.reasonix.desktop`，通知显示名称仍为 `Reasonix`。
它与 Reasonix Studio 的 `io.reasonix.studio`、旧 Tauri 桌面端的
`dev.reasonix.desktop` 分离。Wails Studio v2.10.0 和旧 Reasonix Desktop 曾共用
`Reasonix`，新版 Desktop 不再使用这个共享身份。

## 安装与升级

在任何 Electron 窗口显示前，Desktop 会在永久 launcher 存在时，将窗口的重新启动
命令和图标设为该入口。新建任务栏固定项因此不会依赖版本目录内的 Electron 程序，
也不会依赖启动时临时传入的服务路径环境变量。

安装器在启动 Desktop 前，为刚创建的准确快捷方式路径设置身份。它调用已安装的
launcher 维护命令 `--repair-shortcuts <绝对.lnk路径...>`；该命令只修复已有且属于
本安装的链接，随后退出，不创建窗口、不启动服务或旧版迁移器。

正常启动 launcher 和桌面进程时，也会修复安装目录、个人及公共桌面、个人及公共
开始菜单 Programs 目录（含 Reasonix 子目录），以及当前用户任务栏固定目录内名称
以 Reasonix 开头的已有链接。名称本身不能
证明归属：解析后的目标必须是当前安装内可识别的入口，指向外部的目录联接不被接受。

归属已确认且身份为空或旧 `Reasonix` 的链接会采用新身份。已经使用新身份的链接，
仍可修复过期的目标和图标。明确标注 Studio、Tauri 或未知身份的链接，即便名称为
Reasonix 也会保留原样；其他独立安装不会被修改。

永久入口 `reasonix-launcher.exe` 存在时，指向
`versions/<版本>/reasonix-desktop.exe`、`versions/<版本>/app/Reasonix.exe` 或平铺的 `app/Reasonix.exe`
的链接会迁回永久入口，之后删除旧版本目录也不会让快捷方式失效。修复保留启动参数、
描述、窗口显示状态和用户自定义图标；改写目标时，工作目录设为安装根目录。
仍在使用的平铺 Go 安装保留其有效 Go 入口。

无法读取或写入的链接会记录警告，留待后续启动重试；进程不会退回共享旧身份。
Windows Explorer 可能保留固定项缓存；若链接修复后仍显示为单独图标，可取消固定，
从永久 launcher 启动 Desktop 后重新固定。

## 共存与回退

升级通过既有完整发布单元机制同步交付 launcher、Electron 和 Go 二进制。
回退须恢复完整旧版本。旧 launcher 或旧桌面端可能恢复旧快捷方式身份，再次完整
升级后会重新修复自有链接；不同版本二进制混用不属于身份兼容保证。

旧 `Reasonix` 通知注册和通知历史不删除、不迁移，因为已安装的 Studio 仍可能使用它们。
新版 Desktop 通知使用自己的注册。绑定于旧身份的 Windows 通知偏好不会复制到新身份。

Studio 自身 Electron 运行时和通知身份的统一单列后续修复。Desktop 不修改 Studio
文件，不自动升级或卸载 Studio。

## 合并前验证

运行 Windows 原生应用身份、launcher、通知测试，Electron 类型检查与测试，以及安装器
打包检查。源码检查与模拟调用不能证明 Explorer 最终的任务栏分组行为。

在 Windows 11 上，分别将候选版本与 Studio v2.10.0、当前 Studio 版本同时运行。
使用独立测试安装与数据目录，覆盖首次安装、旧固定项升级、两种启动顺序、固定与取消
固定、从各自固定项重启、最小化与恢复、通知来源隔离，以及删除旧 Desktop 版本目录后
启动。两款产品必须独立分组并启动正确应用；记录实际构建版本，以及安装版和便携版
是否完成验证。
