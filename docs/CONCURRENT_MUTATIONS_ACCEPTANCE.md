---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-22
---

# Concurrency implementation acceptance / 并发实现验收

## 中文

本文汇总 #14025 的实现与本地验收。分批功能、恢复、兼容、三平台及性能检查已完成；
新安装启用 `auto`，已有配置保持原选择。托管 CI、上游合并及发布是独立动作。

### 实现矩阵

| 批次 | 已实现的行为 | 决定性证据及说明 |
| --- | --- | --- |
| A：原缺陷 | 乐观父子写入共用协调路径；保留 Bash/MCP；拒绝的准备撤销新增占用 | 真实 boot 装配、兄弟 claim 及 opaque 工具回归 |
| B：共享事务 | 明确影响类型、规范资源身份、一次申请、公平排队、独立读取版本、锁内 CAS | 多进程协调与 boot task/fleet 同文件/不同文件回归 |
| B：执行边界 | 审批先于申请；hooks 与观察纳入变更；等待和模型额度分离 | 拒绝、部分执行、取消、物理进程退出及 generation 测试 |
| C：自动隔离 | 首次执行自动选择、后台/嵌套 task 与 fleet、共享根预算、声明式依赖复制 | 原生平台、真实 boot 嵌套/续跑/取消及持久根恢复 |
| C：成果保留 | 准备期落盘；未整合修改不按年龄删除；直接父子登记及 int 引用保留 | 真进程死亡、准备登记回收、重复身份与旧写者回归 |
| C：跨聊天续跑 | 根聊天 fork 独立重开；祖先 task 复制并复用；来源可安全清理 | 新 v6 副本、旧 v5 拒写、权限及队列取消回归 |
| D：自动整合 | 原授权内三方基线判断；独立整合候选；输入快照重查 | 受控 Linux/容器检查、源漂移、竞争及冲突保留测试 |
| D：恢复 | 日志、预映像、逐文件替换、带写后版本的回滚、同 ID 恢复 | 真宿主死亡、逐阶段故障注入、外部变化及幂等重放 |
| E：配置与诊断 | 新安装持久选择、旧配置保留、保存/有效值分离、等待事件闭合 | config 全套、serve 只读预览、Desktop/TUI 管理及交互验证 |
| 性能 | 同机交替配对、真实进程/文件、模拟模型、最终字节独立检查 | 产品性能报告的 100ms、500ms 及 OS 锁跟踪 |

使用方式及边界见[协议说明](CONCURRENT_MUTATIONS.md)、
[续跑说明](CONCURRENT_MUTATIONS_CONTINUATION.md)、
[配置迁移](COLLABORATION_DEFAULTS.md)与[兼容矩阵](CONCURRENT_MUTATIONS_PROTOCOL.md)。

### 固定源码与平台证据

测试报告保留输入清单、测试 inventory、实际运行项、跳过项、失败及进程残留。
Windows ARM64 不支持 Go race；该平台记录原生功能测试，不冒充 race 结果。

| 输入批次 | 源码 archive SHA-256 | 验收范围 |
| --- | --- | --- |
| 配置默认策略 v2 | `8179adcec006872ead4b997eb3d853130c92446090d8dae5851c120911a2fe21` | macOS root 全量、所属包 race、Windows 26 项、Linux 所属包 race |
| 祖先续跑 v3 | `87fd9a5743e1f19053e351264a1d1ca5dd776bd0d2ef7fbc73bab878c919ed49` | root 全量、delegation/isolation race、Linux 475 项、Windows 装配/状态/续跑 |
| 当前环境兼容增量 v4 | `05b29c4616564bd37436b90e1b785cdbd39cea65e6b05d5ef7301d8912c57790` | 仅调整已在当前候选中的续跑准入顺序；新增回归，重跑 delegation 及原生平台 |
| 产品性能 v7 | `e0810ccaf5d5afc5989385fdb84e08bb392ed2cd393987b61b9be8394cc7f989` | 120 轮 500ms 工作负载、18 轮 OS 跟踪；100ms 独立批次另列 |

配置默认策略、祖先续跑及兼容增量均已取得终态。Windows v3 的 39 项、v4 的
30 项原生检查全部通过，inventory 完整，无跳过或测试进程残留。两批存在重叠。

原生 Linux v3：delegation 334、isolation 138、真实 boot 装配 3 项全部通过 race，
inventory 完整，没有跳过项。此前受控容器批次另有原生 Windows 客户端、Linux
Engine、宿主死亡和源漂移证据；它不证明 Windows 候选 Bash 已被完整约束。

macOS 首次 root sweep 只有 boot 的整包累计 600 秒超时；其余包全部通过，
包括 isolation 587.912 秒、serve 251.847 秒、control 115.431 秒。
isolation 全套 race 的首次整包运行也达到累计上限，当时当前测试只运行了 2 秒。

保留两次失败栈后，按完整清单分批复验：boot 858 项、isolation race 138 项全部
闭合，无漏项及失败，每批仍使用原 10 分钟限制。boot 有 4 个顶层条件跳过及
1 个 Windows 专用子项跳过，原始报告保留名称；受控容器另有实际后端验收。

最终 v4 delegation 全套 race 37.125 秒通过；新增 builtin 复验 25.146 秒通过。
上游更新至 `studio@a708a88a75`，19 项邻近前端测试、typecheck 及 build 通过。

验证依据是分批固定的源码输入及其变化范围。文档更新、上游纯前端菜单/发布说明
与 benchmark 诊断改进不改变已经验收的生产 Go 协议。最终源码仍需静态及整合检查。

### 失败保留与机制修复

| 首轮失败 | 原因与处置 | 验收原则 |
| --- | --- | --- |
| 共享后台祖先续跑缺少即时引用 | 准备预约混同尚未创建的环境；分开处理并复用已准备 transcript | 保留旧后台行为，不能修改旧断言掩盖回归 |
| Windows 祖先复制字节断言 | 测试仓库继承 Git 自动换行；fixture 固定 `core.autocrlf=false` | 继续检查精确字节，保留正常 Git checkout 规则 |
| Go 包发现时缺少新类型 | 检查在源码解压结束前启动 | 校验完整输入后才启动依赖检查；不是产品修复 |
| macOS 整包累计 600 秒超时 | 记录显示当前测试只运行 2 秒或 9–33 秒；完整 inventory 分批后闭合 | 保留原失败，不增加产品超时，不遗漏检查项 |
| 初版性能 500ms 准备 EOF | 当时 harness 未保留 stderr，原因仍未知 | 保留失败；新版逐轮保存诊断，成功重测不冒充根因解释 |

### 性能与保障边界

PR #14133 的首轮托管检查补充发现：缺少完整隔离后端时，装配测试仍假定创建候选；
现已分别断言可用环境和安全回退，不跳过拒绝路径。工具 `Executed` 表示进入工具执行，
物理程序未启动应检查 Shell 的 `not_run` 状态及无退出码，不能混为一个标志。

另外修复两个生命周期边界：成功的 MCP 延迟握手在发布客户端时登记缓存持久化，
不再依赖可取消的观察者；Linux 候选将宿主已解析的 Git 元数据显式只读映射到私有
`/tmp` 内。真实 Linux 检查覆盖元数据可读不可写、无关临时目录不可见及读取拒绝优先。
同时补齐前端恢复动作登记、英文拒绝提示，并移除路径合并函数的容量加法。

这些增量不把托管 CI 或安全审查改记为通过。首轮 CodeQL 告警仍需逐项核对数据流、
实际授权边界与新旧来源；检查摘要与维护者按规则/文件计数的净增量并非同一指标。

500ms 模拟推理、4 个 worker、每个 3 次变更、每种负载 20 组成对运行：

| 负载 | legacy 中位数 / P95 ms | shared 中位数 / P95 ms |
| --- | --- | --- |
| 不相交明确文件 | 15090.42 / 15140.99 | 6899.42 / 7037.42 |
| 不明范围操作 | 14918.27 / 17473.00 | 6682.40 / 6771.58 |
| 同文件 | 21316.23 / 25187.76 | 11278.70 / 14668.47 |

宿主为 10 个逻辑 CPU；实测 load1 为 34.109–182.376，属于高负载对照。
新版共享事务与推理的文件资源持有重叠为零。同文件冲突均通过重新读取及重调度处理。
详见[完整产品性能记录](CONCURRENT_MUTATIONS_PRODUCT_PERFORMANCE.md)。

worktree 不隔离数据库、端口、凭证及全部共享 Git 元数据。协议不能约束任意外部
写入者；不完整观察及过期证据保持未知。多文件发布是持久日志下的逐文件操作，
不承诺文件系统的跨文件原子提交。

macOS/Windows 可以使用由 Linux VM/WSL 支撑的本地容器 Engine 验证冻结快照。
运行时必须声明并固定；没有可靠验证器就保留成果，不把普通 Shell 成功当成整合凭据。
Windows 普通候选 Bash/hooks 的完整写入边界仍不可用；自动准备提前回退共享短事务，
显式隔离程序执行拒绝。原生平台专用构建及签名不由 Linux 检查代替。

## English

PR #14133's first hosted run exposed additional qualification gaps and two owner
defects. Unsupported-backend assembly tests now assert safe shared fallback or
unknown/refused verification instead of assuming a candidate can execute.

Entering a tool is distinct from starting its shell process; prelaunch refusal
is checked through `not_run` and an absent exit code.

Successful deferred MCP handshakes now register cache persistence when the host
publishes the client, independently of the cancellable lazy observer.

Linux candidates restore host-resolved Git metadata read-only inside their
private temporary namespace. Real backend checks retain read-denial precedence,
metadata write refusal and unrelated temporary-directory isolation.

Frontend action/translation registrations and allocation-hint arithmetic are repaired.
Hosted checks and individual CodeQL flow review remain separate pending gates.

This record covers the #14025 implementation and completed local qualification.
New installations use `auto`; existing configurations retain their choice.
Hosted CI, upstream merging and publication are separate delivery actions.

The implementation separates capabilities, task ownership, mutation leases,
execution environments and verification evidence. Shared writes use short
transactions and actor-specific read versions. Complete path sets and capacity
are admitted together; model waits retain no previous file lease.

Task, fleet, background and nested execution share environment and budget
owners. Retained candidates survive restart and cancellation. Ancestor task
copies preserve the comparison baseline with fresh identities and evidence;
root conversation forks reopen their independent scene.

Integration builds a separate candidate from current source plus the result.
Snapshot-bound controlled checks precede source CAS. Durable per-file journals
support interruption recovery and version-checked rollback. Conflicts retain
both inputs instead of silently selecting a winner.

The source hashes and platform scopes above identify each fixed qualification
input. Linux v3 passed 334 delegation, 138 isolation and 3 assembly tests under
race detection, with complete inventories and no skipped cases.

Windows ARM64 uses native functional tests and does not support Go race.

Portable controlled
verification has separate host-death, source-drift and native Windows-client /
Linux-Engine evidence. Windows v3 passed 39 tests and v4 passed 30 overlapping
incremental tests, with complete inventories, no skips and no survivors.

The initial macOS root command retained a boot cumulative-package timeout;
every other package passed. Boot's complete 858-test inventory and isolation's
138-test race inventory passed in batches under the unchanged 10-minute alarm.

Boot recorded four conditional top-level skips and one Windows-only subtest
skip. The controlled container backend has separate real-platform evidence.
The original timeout results remain failures, not a claimed green root command.

The paired product workload uses 4 workers and 3 mutations per worker. At 500ms
simulated inference, shared median/P95 times improved in all three workloads;
the table preserves exact values and high host load. File-resource/inference
overlap was zero.

The earlier preparation EOF remains unexplained because its
old harness discarded stderr; the failed run remains part of the record.

Worktrees do not isolate databases, ports, credentials or all shared Git state.
External writers can bypass coordination. Incomplete observations and stale
checks remain unknown; filesystem publication is not cross-file atomicity.

macOS and Windows may use a local Linux-VM/WSL-backed container Engine for
frozen-snapshot verification. Missing controlled runtimes retain results.

Windows candidate Bash/hooks still lack full mutation bounds: automatic task
preparation falls back to shared transactions before candidate creation;
explicit unsupported execution refuses. Linux checks do not replace native
platform builds or signing.
