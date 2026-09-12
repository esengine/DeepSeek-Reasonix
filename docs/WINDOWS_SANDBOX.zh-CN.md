# Windows 沙箱架构

Reasonix 在 Windows 的 Shell 和写入进程中采用了 DeepSeek Harness
`c291e7961a` 的受限令牌思路。当前实现是原生 Go 移植，运行时不依赖
Harness 代码。

## 权限模式映射

| 权限模式 | Windows 执行方式 |
| --- | --- |
| 仅可查看 | 使用不携带目录能力 SID 的 `WRITE_RESTRICTED` 主令牌；直接只读工具仍可使用 AppContainer。 |
| 工作区内修改 | `WRITE_RESTRICTED` 令牌只携带当前工作区、已授权额外目录和会话私有临时目录的能力 SID。 |
| 完全权限 | 走宿主正常执行路径；上层的显式禁止和 Reasonix 状态保护仍然生效。 |

后端对外报告为 `windows-write-restricted+appcontainer`，强制等级为
`partial`。权限快照还分别说明写入、读取和网络隔离方式，桌面端和远程端
不会把 Windows 的边界误认为与 Seatbelt 或 bubblewrap 完全相同。

## WRITE_RESTRICTED Shell 通道

Reasonix 按规范化目录、用途和 Reasonix 专用域确定性派生能力 SID。工作区
和会话临时目录使用不同用途，因此 SID 不会复用。Helper 在精确目录上增加
一条可向文件与子目录继承的 Modify 类 ACE，并明确排除 `WRITE_DAC` 和
`WRITE_OWNER`。

ACE 会保留在目录上，真正的授权则属于每次启动的进程令牌。只有宿主把同一
SID 放进限制 SID 集合时，该进程才能使用这条 ACE。降级为“仅可查看”、撤销
额外目录授权、切换会话或轮换临时目录后，新令牌不再携带旧 SID，原 ACE
立即失去作用。Helper 修改 DACL 后会重新检查 Windows 文件对象身份；路径
在准备期间被替换时失败关闭。

修改工作区 DACL 之前，Helper 会在宿主控制的 Reasonix 状态目录内原子写入
`preparing` 记录；精确 ACE 和文件身份复核完成后，同一记录更新为 `active`。
记录包含版本、规范路径、Windows 对象身份、SID、准确掩码、继承方式、用途和
所有者进程。会话临时目录记录不持久化，因为随机 generation 目录删除时会连同
它的 ACE 一起回收。

受限令牌使用 `DISABLE_MAX_PRIVILEGE`、`LUA_TOKEN` 和
`WRITE_RESTRICTED`。限制集合包含登录 SID、Everyone，以及工作区模式下本次
调用获准的能力 SID。保留登录 SID 和 Everyone 是为了让 PowerShell、DLL、
CNG、管道及桌面初始化正常工作。默认 DACL 优先使用会话临时目录 SID。
子进程先以挂起状态创建，加入启用了进程树清理和 UI 限制的 Job Object 后再
恢复；任务停止或超时会终止整棵进程树。

此通道有意不使用 `CREATE_NO_WINDOW`，因为受限 PowerShell 和 Node 的初始化
依赖正常控制台继承。Reasonix 的 Helper 本身仍由桌面进程启动器隐藏。

## 直接工具、禁读和并发

直接只读工具继续使用 AppContainer；Windows 上只有这条通道可以移除网络
能力。`WRITE_RESTRICTED` 不隔离读取，因此已有 `forbid_read` 仍使用临时拒绝
ACE，并保留安全描述符快照、崩溃残留标记、恢复和全生命周期串行化。

能力 ACE 只在创建和复核时短暂持有命名互斥锁。父子路径共享锁域，避免并发
DACL 修改丢失条目。精确 ACE 存在后，同一工作区的普通命令可以并发执行。

获准工作区不能包含 Reasonix 受保护状态目录，任意状态子目录也不能成为写入
根。唯一例外是桌面的直属 `<state>/global-workspace`；能力只授予该精确子目录，
不会授予父目录。会话临时目录必须同时与工作区、受保护目录分离。

## 从旧 Windows 后端迁移

旧 Shell 通道使用低完整性令牌，并递归修改工作区完整性标签。新通道不再给
工作区降完整性级别，也不再在每次命令结束后恢复大型目录树。为直接工具保留
的 AppContainer 兼容代码仍可能使用原有 ACL 和完整性标签处理。

新 Shell 通道不依赖旧低完整性标签。能力 ACE 只有在令牌携带对应派生 SID
时才有效，因此可以跨版本保留；删除工作区或会话临时目录时，对应 ACE 会随
目录一并删除。

Helper 载荷带严格版本号；缺少版本、未来版本、未知字段或尾随数据都会被
拒绝。原生 API 缺失、路径身份变化、受保护目录冲突或无法实现的网络约束都会
返回明确的沙箱失败，Reasonix 不会静默改为无沙箱重试。

## Windows 已知边界

- 受限令牌约束文件写入类访问，不隔离普通读取、网络、注册表、命名对象或
  进程可见性。
- 为兼容 Windows 运行时，Everyone 仍在限制集合中。若某个位置本身明确允许
  Everyone 写入，该位置可能仍可写。
- 对 NTFS 硬链接和特殊 ACL 继承对象仍需原生回归验证。当前会复核目录对象
  身份，但不宣称具备通用硬链接隔离能力。
- AppContainer 可以禁止直接工具联网。Shell 或写入进程要求 `Network: false`
  时会失败关闭，因为受限令牌通道无法执行该网络边界。
- 即使全部原生 API 可用，Windows 的强制等级仍为 `partial`。

## 验证方式

在每个支持的 Windows 架构上运行原生测试和 100 次冷启动／热启动基准：

```powershell
./scripts/verify-windows-sandbox.ps1 -OutputDirectory .codex-build/windows-sandbox-native
```

CI 的 Windows smoke 组已经包含 `reasonix/internal/winsandbox`。非 Windows
主机还会为 Windows amd64 和 arm64 交叉编译 `internal/winsandbox` 与
`internal/sandbox`。交叉编译只能验证绑定和构建标签，不能代替原生 ACL、
令牌、Job Object、PowerShell 和 Electron 验证；执行状态记录在证据说明中。

本实现参考 DeepSeek Harness 的 MIT 许可模块
`packages/sandbox/sandbox-windows-acl`，归属信息见
`internal/winsandbox/NOTICE.md`。
