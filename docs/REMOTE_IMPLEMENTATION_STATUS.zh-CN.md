# Reasonix Remote V1 实现状态

> 冻结基线：[`REMOTE_ARCHITECTURE.zh-CN.md`](./REMOTE_ARCHITECTURE.zh-CN.md)
>
> 最后更新：2026-07-20
>
> 状态原则：只记录已经落地并由测试证明的内容；计划、接口草案和未运行的测试不计为完成。

## 当前阶段

阶段 9：clean `91fd2029` 协调候选已经完成 Windows Wails 与 Linux amd64 双端构建、WSL Host lifecycle
部署、三方 Build ID/SHA256 一致性、全量/定向/race/frontend 门禁和 config-backed System OpenSSH 真实
attach/reconnect。阶段 8 的 clean `89a55a52` 与 dirty 工作树身份冲突已由两个本地提交 `0167f7f39`、
`91fd20297` 解决；下方阶段 8 仍作为历史证据保留，阶段 9 是当前权威状态。

原生 `91fd2029` Desktop 已使用既有 `reasonix-wsl` config 连接到同身份 daemon，显示 Remote
capabilities 并浏览 Linux `/home/taibai`；连接进程是 System32 `ssh.exe`。但本轮 computer-use 在继续附加
Session 前连续两次返回 `failed to activate captured window`，按安全规范停止输入，因此导致本提交的
Remote → Local → 同一 Remote 投影复验尚未取得原生点击式闭环证据，不能宣告整个 Remote V1 实机目标
完成。`/clear` 的最终 transcript 删除仍需动作发生时确认，未执行；密码、2FA、Host Key changed 与外部
物理 Linux 仍属于人工矩阵。direct/no-config 已被用户后续“使用现有 config”指令覆盖，不是本轮阻塞。

开始实现前的工作树与架构一致性预检已经完成。预检时，分支
`codex/remote-feature` 相对 `origin/main-v2` 只有冻结架构文档提交，且不存在
`internal/rpcwire`、`internal/remote`、Remote 生成类型或既有 Remote 实现。因此后续阶段均从
真实的未实现状态推进。

## 已完成内容

### 实现前预检

- 实现开始前已确认分支为 `codex/remote-feature`。
- 实现开始前已确认工作树中仅有三项与 Remote 无关且必须保护的用户内容：
  `site/package-lock.json`、`_go-learn/`、`developer-portal/`；Remote 实现不会修改或清理它们。
- 已逐节核对冻结架构的组件边界、方法注册表、错误表、固定资源上限、七阶段计划和 31 项
  V1 验收标准。
- 实现开始前已校验文档结构连续：22 个一级章节、当时的 RMT-001 至 RMT-045 连续、71 个
  method/notification、51 个领域错误；README 入口有效，冻结文档提交无 whitespace 错误。
- 已确认 Remote Protocol 必须独立于 ACP，阶段 1 只能提取中立 wire 层，不能复用 ACP 的
  connection-owned service 生命周期或有损事件投影。

### 阶段 1（已完成）

- 已从 ACP 等价提取协议中立的 `internal/rpcwire`，提供双向 JSON-RPC request/response、
  notification、结构化 error data 和可分别配置的入站/出站 NDJSON 帧限制。
- ACP adapter 保留原有构造入口、32 MiB 入站限制、连接结束取消 handler 和错误 wire；Remote
  已使用独立的 8 MiB 对称限制。
- 已增加 `rpcwire` 对结构化领域错误、响应错误解码以及入站/出站换行计入帧预算的测试。
- `rpcwire` 已区分 ACP legacy 与 Remote strict JSON-RPC 校验；strict 模式拒绝缺失/错误
  `jsonrpc`、冲突 request/response shape、非法 params、非法 ID 和不完整 error object。
- 已提供 read-loop 同步 `BeforeRequest` gate，使 Remote router 可以按真实 wire 到达顺序强制
  initialize-first，同时保留初始化完成后的并发 handler。
- 已提供协议中立的 response-after-write 回调，并验证回调只在结果帧写入尝试后执行；Host 可据此
  严格实现 `remote/detach` 的“先成功响应，再释放 lease/关闭 transport”，写失败时不会误释放。
- 已建立 RuntimeParityManifest：Desktop/Wails 与 `AppBindings` 当前 295 个方法全部且仅分类一次（128 个
  shared-runtime、65 个 desktop-local、14 个 host-readonly、9 个 deferred-v1、79 个
  out-of-scope）；新增、漏分、重复、非法分类和 stale 条目都会使测试失败。
- 已建立共享 source revision 解析：优先使用构建注入值，否则读取 Go VCS build settings；dirty
  开发构建带 `+dirty`，无 source revision 的构建将由 Remote Build ID 校验拒绝。
- Make CLI/cross、GoReleaser、npm 六平台 CLI 和 Desktop Wails 正式构建入口已统一注入同一
  `SourceRevision`；Desktop 在构建脚本改写 tracked `wails.json` 前冻结 revision。Guard、launcher、
  update helper 和 plugin example 不参与 Remote peer identity，因此未错误注入。
- 已将冻结的 71 个 wire 方法（68 request、3 notification）、全部 params/result DTO、严格 opaque
  ID、51 个领域错误、固定 capabilities/limits 和完整 `eventwire.Event` 编码为单一 Go 注册表。
- Remote router 直接读取该注册表，强制 `remote/initialize` 为第一条 request；握手前 request、任意
  客户端 notification、重复 initialize、未知字段、非法联合与 handler result 漂移均被拒绝。
- Build ID 已严格包含 `productVersion/sourceRevision/protocolVersion/schemaHash`，四个字段任一不同
  都拒绝连接；生产 `SchemaHash()` 直接使用生成常量，不提供范围匹配、force 或 fallback。
- 已实现三类大字段 owner（`SessionEvent`、`HistoryPage`、`SessionSnapshot`）的 descriptor-driven
  RFC 6901 `null` 占位与 raw JSON 回填；Remote 没有复制或裁剪 `eventwire.Event`。Approval subject
  等无界字段已进入中立 externalizable 描述符。
- 已由同一 registry/schema 生成并提交 canonical JSON、完整 SHA-256 和前端 Raw/Hydrated 类型：
  `schema.generated.json`、`schema_hash.generated.go`、`remoteProtocol.generated.ts`。生成物不含时间、
  绝对路径或随机顺序，`-check` 与临时目录重建会逐字节拒绝漂移。当前 schemaHash 为
  `sha256:5d7a9582b014e88f6787c41b577b467610abbfac23ffa3ce61d839fe2e315c48`。

### 阶段 2（已完成）

- 已实现平台中立的 daemon transport server：真实 `rpcwire` router、严格 daemon Build ID、单客户端
  lease/generation、ping、先响应后 detach、capabilities、原子 subscribe/unsubscribe、普通 Turn
  submit、严格 opaque Turn cancel 和完整 `eventwire.Event` notification。
- Controller 与 Turn 由 daemon/runtime 根上下文持有；SSH transport EOF 只移除 subscription，不会
  Cancel 或 Close 已接受任务。重连测试能在同一 runtimeEpoch snapshot 中恢复任务完成后的 seq 与事件。
- lease resume 会使旧 transport 立即变为 `STALE_CONNECTION`；TTL 过期后旧半开 transport 不再收到
  notification，新 lease 会移除所有旧 attachment。leaseId/runtimeEpoch/turnId/subscriptionId/snapshotId
  在 daemon 生命周期内均不复用。
- daemon 与 attach Build ID 不一致返回 `DAEMON_RESTART_REQUIRED`。阶段 2 验收时命令形 Composer
  输入尚未接入，因此明确返回 capability error；该路径随后已在阶段 6 通过共享 RuntimeAPI、真实
  Operation 和 Session lifecycle 完成，不会伪装成普通模型 Turn。
- 已实现 `remote attach --stdio` 的严格首帧 bootstrap 与字节透明 stdio proxy：Desktop/attach
  Build ID 先于 service 状态检查；首帧错误、双向 EOF、取消、8 MiB 入/出站和 stdout 协议纯净均有
  自动化覆盖。
- 已实现 Linux 当前用户 Unix socket endpoint：只接受 `XDG_RUNTIME_DIR` 固定路径、`0700` 父目录、
  `0600` socket、`SO_PEERCRED` 同 UID、独占 flock、active/stale socket 判别及路径替换竞态保护；Host
  核心仍只依赖 `net.Listener`，没有写死 systemd/Linux。
- 已实现生产 `runtimefactory`：opaque target 只经 catalog 解析为 Host 内部路径和已冻结 profile，
  Controller 使用共享 `boot.Build` 组合根恢复真实 Session；同一进程的 runtime 换代共享引用计数 writer
  lease，其他进程不能并发写同一 transcript。除参数隔离测试外，另有不注入 builder、直接运行生产
  `boot.Build` 并关闭真实 Controller 的集成测试。
- 已实现跨平台 `hostapp` 组合根，把持久 catalog、真实 RuntimeFactory、daemon 和 platform service
  endpoint 接成一个 daemon Host epoch；attach transport 断开不会拥有或关闭该组合根。
- 已实现 `reasonix remote serve` 生产 CLI 入口和 Host 配置 profile resolver。resolver 每次按 workspace
  复用 `config.LoadForRoot` 与现有 model/effort/token/approval 规范化语义，并把完整 resolved profile
  持久化给 Session；不读取 Desktop 本地状态或返回 Host secret。
- 已完成真实 Linux 进程内闭环测试：生产 `remote serve` 绑定当前用户 socket，客户端经该 socket
  完成严格 initialize；另一路从持久 catalog 创建 workspace/session，经共享 ControllerFactory 启动
  daemon 并完成 initialize + 原子 subscribe，得到非空 runtimeEpoch snapshot。
- daemon 已接入真实持久 catalog，并在 wire 上完成
  `workspace/browse → workspace/open → workspace/list → session/create → session/list → session/subscribe`；
  客户端不再依赖测试预置 catalog。`session/create` 在 catalog 持久提交后启动真实 RuntimeManager，
  返回实际 target、resolved profile 和 runtimeEpoch。
- `workspace/open` 与 `session/create` 已使用 daemon 级 requestId registry。查询命中先于 epoch/catalog，
  新请求的登记、catalog durable commit、runtime admission 和即时结果提交由 Host catalog mutation
  sequencer 串行；并发重复、响应丢失后跨 transport 重连和参数冲突均有 wire 测试。
- catalog 的身份、输入和确定性状态拒绝会固定重放；`STALE_HOST_EPOCH`、
  `SESSION_PERSIST_FAILED`、`QUERY_FAILED` 及非 Remote 基础设施错误会 Abort，因此同 requestId 可在
  故障修复后重新准入。若 Session 已持久化但 Controller 首次启动失败，则缓存携带已分配 target 的
  `RUNTIME_START_FAILED`，后续 subscribe 只重试该 target 的冷启动，不会重复创建 Session。
- hostapp 集成测试已改为纯 wire 建立 workspace/Session/runtime；阶段 2 最终门禁证明 attach EOF 后
  已接受 Turn 继续由 daemon/runtime 上下文执行，重连后在同一 runtimeEpoch snapshot 中观察完成状态。

### 阶段 3（已完成）

- 已实现 HostEpoch 级 `contentref.Store` 及 daemon 接入：snapshot/event 在发帧前按真实
  lease/target/runtime/snapshot/seq owner 外置；`session/content` 按 256 KiB Base64 分块返回并校验
  offset、总字节数和 SHA-256。
- contentRef 已执行 64 KiB 外置阈值、snapshot/history 2 MiB 预算、event 完整 8 MiB frame 预算、
  单对象 8 MiB 上限、15 分钟 idle/60 分钟 max-age；UTF-8、精确边界和截断均有测试。
- snapshot/history ref 在订阅替换、取消、响应写失败、连接清理、TTL/容量淘汰或 daemon Close 时
  成对释放；event ref 可在相同 lease 的 SSH 重连后继续读取，但新 lease 和 runtime 换代都会拒绝
  旧 owner。
- Session actor 在同一 sequencer 中冻结 `boundarySeq=N`、类型化 live state、accepted Turn 的 provisional
  history prefix、pending Prompt、todo、context/usage、jobs、checkpoint 和 telemetry；getter、投影、时钟及
  回调 panic 都被隔离，失败不会击穿 actor 或提交半成品 ID/state。
- `session/subscribe` 在进入 actor 前预留 daemon 生命周期内不复用的 `snapshotId`，先原子安装只接收
  `N+1` 之后事件的订阅，再在 sequencer 外完成最终投影；响应成功写入后才启动事件泵，因此 snapshot
  response 严格先于排队事件。投影失败与 Host commit 失败都会事务回滚旧订阅、pump 和 owner。
- runtime/target replacement 会发送显式 resync 并保留 terminal subscription 供同 transport 迁移；同 runtime、
  跨 runtime 和响应丢失路径都使用全局不复用 subscription ID，跨 transport 不能探测或消费旧 ID。
- submit/cancel 已接入 daemon 级 requestId registry：registry lookup 先于 epoch/target 校验，实际
  admission、语义状态提交和 Controller 调用在 Session actor 内完成；响应丢失后相同 requestId 重试
  返回首次结果且不会重复调用 Controller，冲突参数返回 `REQUEST_ID_CONFLICT`。
- 已实现独立的不可变 history store，并通过 daemon `session/history` 接线：按可见用户逻辑 turn 反向分页，cursor 绑定
  host/target/runtime/snapshot，保留当前 Desktop 的显示、编辑重放、工具、reasoning、memory citation、
  compaction 和 opaque checkpoint 语义；synthetic/steer 不制造 turn，system prefix 只出现在最老页。
  snapshot owner 在完整外层 `SessionSnapshot`/`HistoryPage` 编码上精确执行 2 MiB 最终 JSON 预算，超限只按
  完整 turn 回退；单一超过 8 MiB 的正文保留该 turn，并带 `object_limit` 截断描述。身份、cursor、TTL 或容量
  失效统一返回 `SNAPSHOT_EXPIRED`，不形成存在性 oracle。
- Session actor 已实现 strict `TrySteer`、Approval 和 Ask：Host 生成不复用的 opaque promptId，Controller
  私有 ID 不出 Host；pending Prompt 在同 runtime snapshot/reconnect 中保留，完整 MCPTrust 只存 Host
  sidecar。allowed decisions 沿用现有安全规则，persistent 精确映射为 allow/session/persist；Cancel、
  TurnDone 和 runtime replacement 会使 Prompt 失效，响应丢失重试不会重复调用 Controller；对应
  `session/steer`、`prompt/approve` 和 `prompt/answer` 已接入真实 wire handler。
- 已完成 Host catalog 的 workspace/Topic/Session 持久生命周期 wire：`workspace/close`、
  `catalog/workspace`、Topic create/rename/delete/trash，以及 Session rename/close/trashList/trash/restore/purge
  均连接真实 catalog 与 RuntimeManager，不使用静态结果或测试预置目录。
- `workspace/close` 使用 RuntimeManager registry 加全部 Session actor 的原子 reservation；任一订阅、Turn、
  Prompt 或 job 存在时整体返回 `WORKSPACE_IN_USE`，持久化失败会 Abort 并恢复原 runtime/epoch，成功后才
  统一释放全部 idle runtime。reserved catalog close 需要实例绑定、单次使用的 capability，普通调用不能
  在 runtime probe 与持久提交之间穿透。
- `session/close` 已在 Session actor 内完成 requestId 登记、lease generation 复检、epoch 校验、idle 判定
  和 disposition 提交。idle runtime 必须先执行真实 Controller `Snapshot()`；失败返回
  `SESSION_PERSIST_FAILED` 并 Abort requestId，同一 ID 可在存储修复后重试；active runtime 返回
  `retained_active` 且不 snapshot、不取消；已知 cold Session 返回 `already_closed`。
- catalog mutation 在 durable revision 已改变时发送且只发送一次 `catalog/changed`，包括提交后清理失败的
  error response；requestId replay、pre-commit failure、no-op 和已完成 disposition 不重复发送。所有当前已接入
  的 Host/Session mutation 都在 `registry.Begin` 前复检 transport generation，响应丢失后的相同 requestId 可跨
  SSH transport 重放，冲突参数不会重复执行。
- 多 workspace、多 Session runtime 可并发存在；单客户端 lease、attachment generation 和 runtime registry
  隔离旧 transport、旧 Controller 迟到事件及过期 owner。随机断线、lease resume/replace、响应丢失与并发重复
  mutation 均有 wire/race/stress 覆盖。
- daemon 重启生成新的 hostEpoch/runtimeEpoch，不恢复可执行 Turn/Prompt；持久 transcript 中已接受但未完成的
  user Turn 被标记为 `host_restarted` interruption，旧 Turn/Prompt/epoch 均不可再作用于新 runtime。
- Checkpoint 由 actor 投影为 runtime 生命周期内稳定且不复用的 opaque checkpointId；删除再创建或投影失败后
  也不复用已铸 ID。Controller 已提供 Turn/Operation 互斥、可取消的一次性 Shell/Compact/Summarize operation
  原语；相关 Remote wire、Checkpoint 与工作台接线随后已在阶段 6 完成，本条不把它们提前计入阶段 3。

### 阶段 4（已完成）

- 已实现独立于 Host 核心的跨平台 `lifecycle.Manager`；非 Linux 明确返回 unsupported，Linux 生产
  backend 拒绝 root，且不会退化到 nohup/tmux、sudo、shell 或 PATH 查找。
- managed binary 使用当前原生 CLI 的同目录临时文件、fsync、SHA-256/size/完整 Build ID manifest 和
  原子 rename；managed 目录、二进制、manifest、unit 与 socket 均检查属主、类型、symlink 和权限。
- install profile 固定精确 Reasonix Home；发现 unit/manifest 属于其他 profile 时拒绝静默切换。
  systemd unit 使用绝对 managed binary、`remote serve`、`Restart=on-failure`、`UMask=0077` 和精确
  `REASONIX_HOME`，不通过 shell、`/usr/bin/env` 或 PATH。
- lifecycle core 已实现 install/start/stop/restart/status/doctor/logs/uninstall：restart 只在新副本完整
  验证后执行 daemon-reload/restart；status/doctor/stop/logs 不改 binary；doctor 只诊断；uninstall 保留
  配置、Session 与未知文件。
- 全部 mutating lifecycle 命令由固定 `unitPath.lock` 的跨进程 `flock` 串行；锁绑定 unit 而非
  Reasonix Home，因此不同 profile/Build 的并发安装不会各自提交半套状态。锁为当前用户拥有的
  `0600` 常驻文件，等待支持 context 取消；status/doctor/logs 保持无锁只读。
- Linux 生产同步直接打开 `/proc/self/exe` 当前 inode，不会在包管理器替换调用路径后把新路径字节
  与旧进程 Build ID 混写。Home、managed 目录和 unit parent 的全部祖先拒绝 symlink 与不可信
  owner；managed artifacts 和 unit 删除均使用固定 dirfd、`O_NOFOLLOW`、inode 复核与 `unlinkat`，
  路径重绑定不会误删外部内容。
- systemd 启动门禁严格校验 `LoadState`、`FragmentPath`、显式空 `DropInPaths`、唯一单条
  `ExecStart`、Environment、UMask、Restart、Type、NeedDaemonReload 和 Transient；install、inactive
  start、restart 在 daemon-reload 后重新查询，只有完全一致才会启动。stop/uninstall 同样拒绝会通过
  drop-in 执行不可信 `ExecStop` 的 loaded definition。路径中的 `$`、`%`、引号和反斜杠按 systemd
  语义编码。
- 已实现生产 Unix-socket daemon Build ID 只读 probe：只使用冻结的 `remote/initialize`/`remote/detach`，
  从严格 mismatch 恢复完整身份并要求连续两轮一致；正常路径不获取 lease，极端 exact initialize 会先
  成功 detach，daemon 重启、HOST_BUSY、畸形响应、超时和不安全 endpoint 均有覆盖。
- `reasonix remote install/start/stop/restart/status/doctor/logs/uninstall` 已全部接入生产 manager 与
  daemon probe；`status --json` 顶层固定报告 `cliBuildId`、`installedBuildId`、`daemonBuildId`，不存在
  的身份显式为 null。所有 lifecycle 输出、诊断和 usage 使用正确 stdout/stderr 与 exit code。
- Linux `remote serve` 在解析 Build ID、endpoint 或构造 Host 前拒绝 EUID 0；非 Linux 保持明确的
  unsupported-platform 边界，不增加 nohup、tmux、sudo 或自动修复 fallback。
- 当前容器没有可连接的 systemd user bus（`Failed to connect to bus: No data available`），因此未伪称
  完成真实 service 启停。真实命令已验证只读 status 路径和受控诊断；完整 systemctl 顺序、unit 内容、
  filesystem 事务与 daemon identity 由 fake runner、真实 Unix socket 和对抗集成测试覆盖，阶段 7
  仍保留普通 Linux 登录会话中的人工 service 启停项。

### 阶段 5（已完成，包括 RMT-046）

- 已实现原冻结基线的 Desktop 非敏感 Remote Host 存储：Host 条目使用稳定随机 Host ID 和
  `clientInstanceId`，保存 SSH alias/label/可选 config 路径及 resume lease；文件为 `0600` 原子替换，
  frontend 不能读取或覆盖 client/lease identity，也不保存密码、私钥口令或 2FA 答案。
- 已实现系统 OpenSSH transport，固定使用 `-T`、`RequestTTY=no`、`StrictHostKeyChecking=ask`、
  `ClearAllForwardings=yes`、`PermitLocalCommand=no` 和 `RemoteCommand=none`，不经过 shell；远端命令固定为
  `reasonix remote attach --stdio`。stdout 只承载协议，stderr 独立限界排空并只投影为结构化安全错误。
- Windows 默认连接不再依赖 GUI 进程恰好继承完整 PATH：Desktop 先验证并使用
  `%SystemRoot%\System32\OpenSSH\ssh.exe` / `%WINDIR%\System32\OpenSSH\ssh.exe`，再回退到
  `exec.LookPath("ssh.exe")` 的绝对结果；显式 SSHPath 与非 Windows 的 PATH 行为保持不变。当前 Windows
  环境已确认 inbox OpenSSH 9.5p2 文件存在、Microsoft 签名有效且可执行；resolver 普通/race 测试、vet 和
  Windows amd64 交叉编译通过。修复版 Desktop 已生成并启动，最终 GUI 点击连接结果仍以本节下方实机项为准。
- 已实现 Desktop 进程的早期 AskPass helper 模式和 loopback broker：每次连接独立 HMAC capability、
  replay/deadline 校验、AES-GCM 响应、prompt 类型化与长度限制；Host Key 变化 fail closed。密码、key
  passphrase 和 verification code 只在内存中一次性交付，取消/超时后失效，日志、Host store 和事件均不
  含回答。
- 已实现生产 Remote typed client 与 adapter：严格 initialize/Build ID、10 秒 heartbeat、30 秒 lease、
  detach response/EOF 顺序、连接 generation、atomic subscribe/history/contentRef、snapshot migration、
  结构化 fault 和 transport 原地重连。显式 attach 始终取得 fresh snapshot；旧连接排队 event/fault 由
  generation 隔离，不能产生 ABA 污染。
- 已实现 TargetManager 六状态机及唯一 Target 约束：Local → Remote 前原子检查全部本地 runtime、Prompt
  和 job；Remote → Local 必须显式确认；连接失败不 fallback；switch/reconnect 的旧异步结果按 generation
  丢弃。shutdown 区分 committed detach 与未知 transport outcome，未知结果保留 resume lease。
- 已实现 LocalRuntimeAdapter 的真实 suspend/resume：覆盖可见与 detached runtime，并通过 admission
  barrier 关闭切换 TOCTOU；Local tab/controller/layout/session identity 在 Remote 期间保存，恢复 Local 时
  所有 Controller 先恢复再发布 `LocalConnected`。Local 与 Remote 均通过共享 `runtimeapi.RuntimeAPI`
  Phase 5 合同进入工作台。
- 已实现 Remote Host 目录选择及一个 workspace/Session 的真实工作台闭环：支持默认目录、typed path、
  opaque directoryRef 分页/父子导航、primary 与去重的多个 additional dirs、Host 默认 profile 和新 Topic；
  `open → create → atomic attach` 全部成功后才发布 tab。create 已成功而 attach 失败时保留 pending identity，
  相同 UI 重试不会创建第二个 Session。
- 已把该 Remote Session 接入现有 tab/meta/history/checkpoint/Composer/Steer/Cancel/Approval/Ask/event 路由；
  close tab 执行真实 unsubscribe 而不删除 Host Session。Remote opaque ID 不写入 `SessionPath`，Linux
  `displayPath` 不写入本地 `WorkspaceRoot` 或交给 Windows 本地文件 API。阶段 5 验收时尚未接入的文件树
  明确为空，不用静态数据伪装；阶段 6 随后接入真实 Host 文件树、搜索、预览、Workspace changes 与只读 Git。
- 已实现 Host CRUD、connect/reconnect/Local switch、全局连接状态、secure AskPass modal、Remote workspace
  setup 和三语 UI；成功创建 Session 后回到现有工作台，没有建立第二套 chat 页面。
- 已实现最多 200 条、时间有序的结构化连接生命周期日志。UI 只显示时间、状态、Host label 和安全错误，
  不显示 Host ID、SSH argv、raw stderr、client/lease identity 或 AskPass 回答；返回 slice 与内部存储隔离。
- 当前 Linux 容器不能运行 Windows Wails GUI，也没有可连接的外部 SSH Host，因此没有伪称完成实机
  Windows → Linux 操作。生产代码经过 Windows/amd64 交叉编译；SSH argv/AskPass/daemon client/workbench
  闭环由真实进程 transport、协议 client 和自动化 adapter 测试覆盖。真实 Windows GUI → 普通 Linux
  Host 仍按冻结计划作为阶段 7 必须执行并逐项记录的人工验收。
- 已实现 RMT-046：Host 条目明确使用 `mode=direct/config`；默认 direct 保存
  `destination=username@host` 与独立 `1..65535` port，无需用户手写 SSH config；config 保存
  alias/sshConfigPath，v1 store 旧条目迁移为 config 且不得失效。直接目标必须先解析 username、Host
  和 port，以固定 `-l`/`-p`/`--` argv 无 shell 启动 `ssh`；拒绝空值、空白、控制字符、
  option-shaped Host 和越界端口，IPv6 使用 `username@[addr]`，不接受内嵌端口。该变更只涉及 Desktop
  Host store/UI/SSH argv，不改变 Remote wire、schemaHash、Build ID、daemon、lease 或 Session 状态。
- direct 模式默认端口为 22，显示名称可留空并由后端使用规范化 destination；高级 config 模式与旧
  frontend 的省略 mode 输入继续兼容。store v2 持久化新字段，v1 条目只按 config 语义读取，首次后续
  mutation 原子写回 v2，不猜测或改写原 alias。

### 阶段 6（已完成）

- 已建立完整、目标中立的 `runtimeapi.V1RuntimeAPI`：21 个聚焦领域接口覆盖冻结的 Connection、Host、
  Workspace/Catalog/Topic/Session、History/Content/Composer、Operation/Profile/Goal/Shell、Context/Balance/
  Jobs、Memory/Research、File/Git 面；DTO 不依赖 ACP 或 Remote protocol，也不泄漏 requestId、epoch、
  subscription、seq、tabId 或路径身份。反射测试固定方法集合并拒绝 transport/Desktop 字段漂移。
- 已实现 Desktop `RemoteRuntimeAdapter` 的完整 V1 typed mapping，并由编译期断言保证未漏接口；所有
  Remote mutation（包括 Host、Session record、Session 与 Memory/Research）统一经过 mutation journal。
  transport outcome 未知时不会自动重发；只有用户显式重复同一语义操作且 Host/runtime epoch 未变时
  才复用同一 requestId，确定性响应会立即清除记录，epoch 变化生成新 ID。
- 已完成共享 `runtimeservice.FileGitService`，Local/Remote 可复用相同的路径、分页、搜索、预览、
  Workspace changes 与只读 Git 规则：primary root 与 symlink containment、HMAC opaque cursor、当前噪声
  过滤、10,000 项搜索扫描上限、256 KiB UTF-8 安全文本预览、媒体仅返回 metadata、checkpoint/Git
  合并且不泄漏原始 Git 错误、100 条 full-hash/RFC3339 history、commit file 分页及 1 MiB streaming
  patch 上限均有对抗测试。六个 daemon wire handler 已接入真实 catalog workspace root、当前
  SessionRuntime epoch 与 actor-owned checkpoint changes，并验证路径逃逸、symlink、游标篡改/过期、
  Git 不可用和资源截断；Host 查询与 Desktop 文件/Git 面板路由均已形成真实闭环。
- 已接入一次性 Shell、Compact、Summarize 与 Operation Cancel 的真实 Host actor/wire：Operation ID 在
  daemon 生命周期内不复用，工作由 daemon-owned runtime 持有而不跟随 attach/RPC context 终止，取消严格
  命中当前 opaque ID；snapshot 与 event 同时投影 `currentOperation`/`operationId`。四个 mutation 统一经过
  lease、epoch、requestId 幂等门禁，Compact/Summarize/Shell 均调用真实 Controller operation，而非 mock。
- RuntimeParityManifest 已同时核对 Go Wails 导出面、前端 `AppBindings` 和 manifest；三者新增、删除或
  分类漂移都会失败。分类本身不代替运行测试；最终接通由下述阶段 6/7 证据证明。
- `runtimeapi.V1RuntimeAPI` 的 68 个方法已由 Local 与 Remote adapter 完整实现；冻结的 71 个协议方法
  均接入真实 router/handler（68 request、3 notification），不存在 mock、空 handler 或静态结果。
  Local/Remote 共用同一 DTO、错误、分页/截断与 `RuntimeService` 业务规则，Remote adapter 不复制规则。
- Workspace、Topic、Session、回收站以及多 workspace/多 Session/tab 已对齐；New、Clear、Fork、
  Rewind、Compact、Summarize 全部通过真实 catalog、Controller 或 Operation 路径执行。
- Composer、Turn、Steer、Cancel、Approval、Ask 已完整对齐；每个 Remote tab 都会重放其 pending Prompt，
  迟到 prompt/turn/operation/checkpoint opaque ID 和旧 generation 不能作用于新状态。
- Profile、Goal、todo、context、checkpoint、jobs、balance、一次性用户 Shell、Prompt history、slash args、
  Memory 与 AutoResearch 已连接共享 RuntimeAPI；Host config/catalog/status 只提供安全只读投影，不返回 secret。
- 文件树、搜索、文本预览、Workspace changes、Git history、commit files 和单文件 patch 已接入现有工作台；
  Remote scope/local-path guard 阻止 Linux display path 进入 Windows 本地文件 API，媒体正文、SFTP、PTY、
  Git 写操作及通用文件传输仍保持后置。

### 阶段 7（当前环境自动化已完成）

- 已建立协调发布门禁：CLI、attach、daemon 与 Desktop 使用相同 source revision/Build ID，CI 校验 tag commit
  与生成 schema；没有 force、降级或自动 fallback。本次没有执行发布、push 或创建 PR。
- 已完成代码级 Desktop → Linux Host 端到端：生产 Host/runtime、Unix socket、attach/client 与 Desktop
  adapter 覆盖断线重连和 Desktop backend 冷重启；测试中的 SSH stdio 使用 `net.Pipe` 模拟，因此不伪称
  Windows GUI 或外部 OpenSSH 实机。
- 已用真实生产 `reasonix remote serve` 可执行文件完成 daemon `SIGKILL` 冷恢复：已接受 user transcript
  保留，host/runtime epoch 更新，活动 Turn 只标记一次 `host_restarted`，旧 Turn/Prompt/epoch 均被拒绝。
- 顶层 durable Turn 已形成三阶段边界：先写 admission marker 并持久提交稳定 user transcript，再执行
  hooks/Goal/AutoResearch/provider/tool，最后持久提交 transcript snapshot 后清 marker。恢复修复或清理失败
  会拒绝发布 runtime；嵌套 plan/goal 不会清除顶层 marker 或重复追加 accepted user。
- 已完成根模块与 Desktop 的普通、race、vet、协议生成、跨平台编译和前端 typecheck/test/build 总回归；
  当前环境不能执行的 systemd user bus、Windows Wails GUI 与外部 SSH 实机项列在下方，不计为已实机通过。

## 测试证据

以下命令使用仓库 `toolchain go1.26.5` 对应的 Linux amd64 Go 工具链，并把临时构建/模块缓存
放在 `/tmp`，未修改用户工作树：

```text
env GOMODCACHE=/tmp/reasonix-gomodcache GOCACHE=/tmp/reasonix-gocache \
  /tmp/reasonix-go1.26.5/go/bin/go test ./internal/acp ./internal/cli

ok  reasonix/internal/acp  2.296s
ok  reasonix/internal/cli  37.290s
```

这是阶段 1 开始前当时的兼容性基线，不把后续 Remote 阶段提前计为通过。

首批 `rpcwire` 提取后的定向验证：

```text
env GOMODCACHE=/tmp/reasonix-gomodcache GOCACHE=/tmp/reasonix-gocache \
  /tmp/reasonix-go1.26.5/go/bin/go test ./internal/rpcwire ./internal/acp

ok  reasonix/internal/rpcwire  0.003s
ok  reasonix/internal/acp      1.741s
```

该次增量运行当时只证明 `rpcwire` 提取和 ACP 定向回归通过；阶段 1 的注册表、schema、Build ID、
router 和 parity 最终门禁见下文。

后续已通过的阶段 1 定向验证：

```text
env GOMODCACHE=/tmp/reasonix-gomodcache GOCACHE=/tmp/reasonix-gocache \
  /tmp/reasonix-go1.26.5/go/bin/go test ./internal/buildinfo ./internal/eventwire ./internal/remote/parity

ok  reasonix/internal/buildinfo
ok  reasonix/internal/eventwire
ok  reasonix/internal/remote/parity

env GOMODCACHE=/tmp/reasonix-gomodcache GOCACHE=/tmp/reasonix-gocache \
  /tmp/reasonix-go1.26.5/go/bin/go test -race ./internal/rpcwire ./internal/acp

ok  reasonix/internal/rpcwire
ok  reasonix/internal/acp

env GOMODCACHE=/tmp/reasonix-gomodcache GOCACHE=/tmp/reasonix-gocache \
  /tmp/reasonix-go1.26.5/go/bin/go test ./internal/buildinfo

ok  reasonix/internal/buildinfo
```

RuntimeParityManifest 还单独通过了 `go test -race ./internal/remote/parity`。这些结果是阶段 1
当时的组成部分，不把后续 Host 或 Desktop Remote 实现提前计入。

阶段 1 最终门禁与当前 Host 核心定向回归：

```text
env GOMODCACHE=/tmp/reasonix-gomodcache GOCACHE=/tmp/reasonix-gocache \
  /tmp/reasonix-go1.26.5/go/bin/go test -count=1 \
  ./internal/buildinfo ./internal/eventwire ./internal/rpcwire ./internal/acp \
  ./internal/remote/protocol ./internal/remote/protocolgen ./internal/remote/parity \
  ./internal/remote/idempotency ./internal/remote/host ./internal/remote/daemon

ok  reasonix/internal/buildinfo
ok  reasonix/internal/eventwire
ok  reasonix/internal/rpcwire
ok  reasonix/internal/acp
ok  reasonix/internal/remote/protocol
ok  reasonix/internal/remote/protocolgen
ok  reasonix/internal/remote/parity
ok  reasonix/internal/remote/idempotency
ok  reasonix/internal/remote/host
ok  reasonix/internal/remote/daemon

env GOMODCACHE=/tmp/reasonix-gomodcache GOCACHE=/tmp/reasonix-gocache \
  /tmp/reasonix-go1.26.5/go/bin/go test -count=1 -race <同一组 packages>

全部通过。

env GOMODCACHE=/tmp/reasonix-gomodcache GOCACHE=/tmp/reasonix-gocache \
  /tmp/reasonix-go1.26.5/go/bin/go run ./cmd/remote-protocol-gen -check

Remote protocol artifacts are up to date.
```

前端生成类型还通过了仓库本地 TypeScript/CSS/Vite 正式构建链：`tsc --noEmit` exit 0，4295
modules transformed，Vite `built in 13.82s`。当前环境的 `pnpm` 默认 store 指向不可写的 Home；
使用 frozen lockfile 与 `/tmp` store 准备现有 `node_modules` 后直接执行同一仓库脚本，未修改 lockfile。

阶段 2 当前生产组合与 attach/service 定向证据：

```text
go test -count=50 ./internal/remote/attach ./internal/remote/service
go test -race -count=1 ./internal/remote/attach ./internal/remote/service
go vet ./internal/remote/attach ./internal/remote/service

全部通过；service 另通过 GOOS=windows/amd64、darwin/arm64 交叉编译，attach 通过
GOOS=windows/amd64 交叉编译。

go test -count=1 ./internal/remote/runtimefactory ./internal/remote/hostapp \
  ./internal/remote/profileconfig ./internal/cli
go test -race -count=1 ./internal/remote/runtimefactory ./internal/remote/hostapp \
  ./internal/remote/profileconfig ./internal/cli
go vet ./internal/remote/runtimefactory ./internal/remote/hostapp \
  ./internal/remote/profileconfig ./internal/cli

全部通过；其中 CLI 测试真实绑定 Linux 当前用户 Unix socket 并完成 daemon initialize，hostapp
测试从持久 catalog Session 经共享 ControllerFactory 完成 subscribe snapshot。
```

阶段 2 最终 wire/断线/并发门禁与并行基础门禁：

```text
go test -count=1 ./internal/remote/daemon ./internal/remote/hostapp
go test -race -count=1 ./internal/remote/daemon ./internal/remote/hostapp ./internal/cli
go test -count=20 ./internal/remote/daemon
go test -count=25 ./internal/remote/contentref
go vet ./internal/remote/contentref ./internal/remote/daemon ./internal/remote/hostapp
go run ./cmd/remote-protocol-gen -check

全部通过；其中 daemon 压力运行覆盖 catalog 并发准入、响应丢失重连、持久化修复、runtime 首次
启动失败、contentRef owner 生命周期和 attach EOF 后任务继续执行；schema/hash/生成类型无漂移。
```

阶段 3 独立组件合并后的整组普通回归：

```text
go test -count=1 ./internal/remote/...

attach、catalog、contentref、daemon、history、host、hostapp、idempotency、parity、profileconfig、
protocol、protocolgen、runtimefactory 和 service 全部通过。
```

history 另通过完整 race、2003-turn stress、Windows amd64 交叉编译和 `go vet`；Host Prompt/Steer
另通过 `go test -race -count=10 ./internal/remote/host`、Windows/Darwin 交叉编译和整组 Remote vet。
这些是 daemon 最终 wire 接线之前的组件证据；完整 wire 验收记录在下方阶段 3 最终门禁中。

Phase 3 catalog 生命周期定向与整组证据：

```text
go test -count=1 ./internal/remote/host ./internal/remote/daemon \
  ./internal/remote/catalog ./internal/remote/profileconfig
go test -race -count=1 ./internal/remote/host ./internal/remote/daemon \
  ./internal/remote/catalog ./internal/remote/profileconfig
go test -count=20 ./internal/remote/host -run 'WorkspaceCloseReservation|SessionClose'
go test -count=10 ./internal/remote/daemon \
  -run 'CatalogLifecycle|WorkspaceClose|SessionClose|CatalogChanged'
go vet ./internal/remote/host ./internal/remote/daemon \
  ./internal/remote/catalog ./internal/remote/profileconfig
go test -count=1 ./internal/remote/...

全部通过。主线程另独立重跑了 catalog race，以及四个新增 lifecycle wire 测试，均通过。
```

阶段 3 最终 snapshot/history/recovery/迁移门禁：

```text
go test -count=1 ./internal/remote/attach ./internal/remote/catalog \
  ./internal/remote/contentref ./internal/remote/daemon ./internal/remote/history \
  ./internal/remote/host ./internal/remote/hostapp ./internal/remote/idempotency \
  ./internal/remote/liveview ./internal/remote/parity ./internal/remote/profileconfig \
  ./internal/remote/protocol ./internal/remote/protocolgen ./internal/remote/runtimefactory \
  ./internal/remote/service ./internal/remote/snapshotcapture ./internal/remote/snapshotowner

全部通过；其中 history 16.169s、snapshotowner 22.147s。

go test -race -count=1 ./internal/rpcwire ./internal/remote/contentref \
  ./internal/remote/daemon ./internal/remote/history ./internal/remote/host \
  ./internal/remote/snapshotowner

全部通过；其中 daemon 12.593s、history 147.425s、snapshotowner 189.239s。

go test -race -count=20 ./internal/remote/daemon \
  -run 'TestSubscribeOwnerFailureRollsBackAndRetainsPreviousOwner|TestSubscribeHostCommitFailureRestoresTransportAndPreviousOwner'

全部通过。另已通过 replacement/history/content/detach `-count=30`、detach response-order
`-count=300`、rpcwire `-count=50`、整组 Remote vet 和 daemon/protocol/rpcwire Windows amd64
交叉编译。

go test -count=1 ./internal/acp ./internal/rpcwire
go run ./cmd/remote-protocol-gen -check

ACP/rpcwire 通过；生成器报告 `Remote protocol artifacts are up to date.`
```

阶段 4 lifecycle、daemon probe 与 CLI 最终证据：

```text
go test -count=1 ./internal/remote/lifecycle ./internal/remote/daemonprobe ./internal/cli

ok  reasonix/internal/remote/lifecycle   1.060s
ok  reasonix/internal/remote/daemonprobe 0.071s
ok  reasonix/internal/cli                36.349s

go test -race -count=3 ./internal/remote/lifecycle
go test -race -count=10 ./internal/remote/daemonprobe
go test -race -count=3 ./internal/cli \
  -run 'Test(Remote|CurrentRemoteBuildID|ProductionRemoteServe)'
go vet ./internal/remote/lifecycle ./internal/remote/daemonprobe ./internal/cli

全部通过；lifecycle 另由实现任务通过 `-race -count=10`，最终修改后再通过一次 race。41 个顶层
lifecycle 测试及其子测试覆盖跨 profile 锁、context 取消、当前 executable inode、全部路径祖先、
symlink/rebind、unit inode 固定、未知内容保留、reload failure、drop-in、缺失/重复 systemd property、
多 ExecStart、`ignore_errors`、not-found 和特殊字符路径。

GOOS=windows/amd64、darwin/amd64 的 lifecycle test compile，以及 Windows amd64 的 daemonprobe 和
完整 CLI test compile 全部通过。`git diff --check` 与相关 Go 文件 `gofmt -d` 无输出。

go run -ldflags=-X=reasonix/internal/buildinfo.SourceRevision=<40-hex> \
  ./cmd/reasonix remote status --json

exit 0；stdout 为单一 JSON object，三个顶层 Build ID 字段均存在；stderr 准确报告当前环境的
`Failed to connect to bus: No data available`、lingering disabled 和 daemon socket 不存在。该命令没有
安装、启动、同步或修复 Host。当前环境没有 systemd user bus，因此真实 unit enable/start/stop 留作
阶段 7 的普通 Linux 用户人工验证项，不能计为本环境已实机通过。
```

阶段 5 Desktop/SSH/adapter/工作台最终证据：

```text
cd desktop
go test -race -count=10 -run '^TestRemoteRuntimeAdapter' .
go test -race -count=3 -run '^TestTargetManager' .
go test -race -count=5 -run '^TestRemoteWorkbench' .
go test -race -count=5 -run '^TestRemoteAppConnectionLogs' .
go test -race -count=3 -run 'TestRemoteApp|TestRemoteRuntimeAdapter|TestTargetManager' .

全部通过。定向用例覆盖 browse/open/create/subscribe/submit/event、Steer/Cancel/Approval/Ask、
断线重连 snapshot、pending Prompt、旧 event/fault ABA、fresh replace-subscribe、detach lease 语义、
未知 mutation outcome 不自动重发、pending-create 防重复、target/Host generation 竞争、真实
unsubscribe，以及连接日志的顺序、200 条边界和 secret non-leakage。

go test .
ok  reasonix/desktop  77.367s

go vet ./...
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c -o /tmp/reasonix-desktop-phase5.test.exe .

均通过。
```

RMT-046 最终增量门禁：

```text
cd desktop
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c .

全部通过。focused normal/race 另覆盖 direct 目标解析与规范化、DNS/IPv4/括号 IPv6、端口
1/22/65535 边界、option/空白/控制字符/内嵌端口拒绝、精确 SSH argv、direct 不传自定义 `-F`、
config 兼容、v1 store 迁移、重复连接身份、CRUD 模式切换以及 client/lease identity 保持。

go run ./cmd/remote-protocol-gen -check
Remote protocol artifacts are up to date.
```

协议生成检查证明此次 Desktop 本地配置变更没有引入 Remote schema、生成类型或 schemaHash 漂移。

当前 Linux 系统 OpenSSH 还用与生产 direct 模式同形的 argv 执行了非连接型解析检查：

```text
ssh -G -T -o RequestTTY=no -o StrictHostKeyChecking=ask \
  -o ClearAllForwardings=yes -o PermitLocalCommand=no -o RemoteCommand=none \
  -l reasonix -p 2222 -- 192.0.2.10 reasonix remote attach --stdio

exit 0；关键解析结果为 user=reasonix、hostname=192.0.2.10、port=2222、requesttty=false、
clearallforwardings=yes、permitlocalcommand=no、stricthostkeychecking=ask。
```

该检查只证明当前 Linux OpenSSH 接受并正确解析固定 argv，不替代 Windows GUI 到外部 Linux Host 的
认证与连接实机验收。

阶段 6 最终功能对齐证据：

```text
go test ./internal/runtimeapi
go test -race -count=10 ./internal/runtimeapi
go vet ./internal/runtimeapi

go test -count=3 ./internal/runtimeservice
go test -race ./internal/runtimeservice
go vet ./internal/runtimeapi ./internal/runtimeservice

go test -race -count=10 ./internal/remote/parity
go vet ./internal/remote/parity

go test -count=1 ./internal/remote/host ./internal/remote/daemon ./internal/runtimeservice
go test -race -count=1 ./internal/remote/host ./internal/remote/daemon ./internal/runtimeservice
go vet ./internal/remote/host ./internal/remote/daemon ./internal/runtimeservice

cd desktop
go test -race . -run 'TestRemoteRuntimeAdapter|TestRemoteWorkbench|TestRemoteMutationJournal' -count=5

全部通过。RuntimeAPI contract drift、Desktop/前端/manifest 三向覆盖、unknown-outcome requestId
显式重试、File/Git Host wire、Operation 生命周期、全部 Host 业务 handler、完整 Local/Remote 工作台
路由以及路径/游标/资源上限均进入自动化；完整总回归见下方阶段 7 门禁。
```

Desktop frontend 使用仓库现有依赖完成以下门禁：

```text
npm run typecheck
npm run test:typecheck
npm run check:css
npm run test
npm run build

全部以 exit 0 通过；RMT-046 最终文件上的 Remote focused UI 共 46 项检查。默认 direct 表单仅填写
`username@host` 即可保存，端口预填 22、显示名称可选；高级 config/旧 alias 编辑、后端默认 label、
目录 browser、additional dirs、Topic/Session 创建、AskPass modal、连接状态、安全只读配置、结构化日志
与 Remote 工作台状态均进入正常测试套件；production Vite build 成功。

紧接该 production frontend build，Wails 2.12.0 未跳过 bindings 生成，并复用刚验证的 `dist` 完成
Windows amd64 application 交叉编译：

```text
wails build -platform windows/amd64 -m -nosyncgomod -nopackage -nocolour \
  -s -o reasonix-desktop-direct.exe

Generating bindings: Done.
Compiling application: Done.
Built desktop/build/bin/reasonix-desktop-direct.exe in 8.315s.
SHA256 813151eb43987ea8e115c5109378e2e5be317812487fe6b83f5c0ae798197f8f
```

这里的 `-s` 只避免 Wails 再次调用仓库配置中的 pnpm install/build；frontend 已由上一组正式脚本即时
构建，bindings 生成和 Windows application compile 均未跳过。该证据证明当前代码可生成 Windows exe，
不等同于已在原生 Windows 上启动 GUI 或连接外部 Host。生成 bindings 后再次运行 frontend
`typecheck` 与 `test:typecheck`，两者均以 exit 0 通过。
```

阶段 7 当前环境最终门禁：

```text
# 根模块普通与 race 全量；package 集合等价于 ./...
TMPDIR=/var/tmp/r/t go test -count=1 <除 cli/service 外的全部根模块 packages>
TMPDIR=/tmp go test -count=1 ./internal/cli ./internal/remote/service
TMPDIR=/var/tmp/r/t go test -race -count=1 <除 cli/service 外的全部根模块 packages>
TMPDIR=/tmp go test -race -count=1 ./internal/cli ./internal/remote/service

# Desktop Go 普通、race、vet
cd desktop
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...

# 协议与静态检查
go vet ./...
go run ./cmd/remote-protocol-gen -check
bash -n scripts/verify-coordinated-release-tags.sh

# 跨平台边界
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build ./internal/remote/...
cd desktop
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c \
  -o /tmp/reasonix-desktop-final.test.exe .
```

全部通过。根模块需要真实 Git fixture 的包使用用户允许的 `/var/tmp/r/t` 与 `/var/tmp/r/c` 隔离运行；
Linux Unix socket 约 108-byte 路径上限使 `internal/cli`/`internal/remote/service` 改在短 `/tmp` 路径
单独运行，普通与 race 均通过。该分区只规避测试临时路径长度，不跳过任何 package。

协议最终门禁确认 71 个方法（68 request、3 notification）、51 个错误、68 个 `V1RuntimeAPI` 方法和
295 项 RuntimeParityManifest；`remote-protocol-gen -check` 报告生成物无漂移，schemaHash 为
`sha256:5d7a9582b014e88f6787c41b577b467610abbfac23ffa3ce61d839fe2e315c48`。ACP、rpcwire、protocol、
protocolgen、parity 与 RuntimeAPI 的普通/race 回归均通过。

生产闭环与崩溃恢复证据：

```text
cd desktop
go test -count=1 . -run \
  'TestRemoteDesktop(ToLinuxHostDisconnectReconnect|BackendRestartResumesLinuxHostSession)EndToEnd'

go test -count=10 ./internal/cli \
  -run '^TestReasonixExecutableRemoteServeSIGKILLColdRecovery$'
go test -race -count=5 ./internal/cli \
  -run '^TestReasonixExecutableRemoteServeSIGKILLColdRecovery$'
```

两个 Desktop E2E 分别为
`TestRemoteDesktopToLinuxHostDisconnectReconnectEndToEnd` 和
`TestRemoteDesktopBackendRestartResumesLinuxHostSessionEndToEnd`；它们使用真实 Linux
Host/runtime/Unix socket/attach/client，但以 `net.Pipe` 模拟 SSH stdio，不是 Windows GUI/外部 SSH 实机。
生产可执行文件 SIGKILL 用例普通 10 次、race 5 次全部通过。另有 focused agent/control/host/runtimefactory/
hostapp 普通与 race、durable admission/recovery 压力测试，证明 user transcript 在任何 hook/provider/tool
前权威提交、最终 snapshot 后才清 marker，恢复失败拒绝发布 runtime，accepted user 在冷恢复后保留且
只产生一次 `host_restarted` 中断。

Linux 上尝试完整 Wails 应用构建时，代码已进入原生依赖解析阶段，但当前容器缺少
`gtk+-3.0`、`gio-unix-2.0`、`webkit2gtk-4.1`、`libsoup-3.0` 开发包；这不是 Go/TypeScript/Remote
实现失败，也没有在未获授权时安装系统包。

### 阶段 7 缺陷修复复验（2026-07-18）

针对 Windows Desktop 实机使用反馈，已完成以下真实修复，不是 mock 或静态界面替换：

- 模型选择器不再把 Remote Topic 标题当作模型名。`MetaForTab` 与 `ListTabs` 的乐观元数据均从
  Host `SessionSnapshot.Profile.Model` 投影与 catalog 一致的模型标签；provider 前缀只移除一层，
  OpenRouter 等包含嵌套路径的模型名不会被截断。
- Remote ProjectTree 与 Local 的 Session 语义对齐：Topic 只有一个 Session 时由 Topic 行承载，
  达到两个 Session 后才显示具体子节点，因此一个真实 Session 不再显示为两个。Session 与回收站
  列表会分页聚合所有 Host Workspace，并携带 `WorkspaceRoot`。
- Desktop 历史、Tab 与 ProjectTree 现在使用 versioned base64url token 无损携带完整
  `WorkspaceID + SessionID`。旧 raw SessionID 仍可兼容解析，但跨 Workspace 重名时会拒绝为歧义，
  不再把 active Workspace 错套到另一 Session。
- `/new` 与 `/clear` 的 ordered SnapshotUpdate 会替换 opaque SessionRef，进而替换 backend tab ID。
  前端现在只在权威 `ListTabs` 已移除旧 Tab、且新 Tab 是 backend active 时迁移状态并完整 hydrate；
  背景 `ready/rebuilt` 不会抢焦点，RPC 与 SnapshotUpdate 两种到达顺序均有自动化覆盖，Local 的
  tab-scoped Session 操作语义保持不变。
- Remote 状态下的“新建项目”不再调用 Windows 本地目录选择器。入口先读取 TargetManager：Local
  使用系统 picker，Remote 打开已有 Host 目录 browser 与 `CreateRemoteWorkspaceSession`；连接过渡态
  明确拒绝。Go backend 也会 fail closed，防止其他调用路径绕过前端。创建 RPC 进行中禁止关闭窗口，
  避免界面看似取消但 Host 实际已经创建 Session。

本次最终文件上的验证证据：

```text
# Desktop Go（完整包）
cd desktop
go test -count=1 ./...                         # PASS, 68.971s

# 变更面的 race + Linux Host E2E
go test -race -count=1 . -run '<model/session/picker/Linux E2E cases>'
# PASS

# ACP、rpcwire、RuntimeAPI、协议注册表/生成器/parity（含 race）
go test -race -count=1 ./internal/rpcwire ./internal/acp ./internal/buildinfo \
  ./internal/eventwire ./internal/runtimeservice
go test -race -count=1 ./internal/remote/protocol ./internal/remote/protocolgen \
  ./internal/remote/parity ./internal/runtimeapi ./internal/remote/client ./internal/remote/daemon
go run ./cmd/remote-protocol-gen -check
# 全部 PASS；生成器报告 Remote protocol artifacts are up to date.

# Desktop frontend
npm run typecheck
npm run test:typecheck
npm run test
npm run build
# 全部 exit 0；Remote target UI 53/53，new/clear replacement 28/28，
# tab switch 75/75，ready 6/6，prompt lifecycle 78/78。
```

使用同一协调 Build ID 完成了 Linux CLI/daemon 与 Windows amd64 Desktop 编译：

```text
productVersion: desktop-v1.17.12-264-g276a52fa
sourceRevision: 276a52fac9ab3b9a8139fafd6a3d4ae9767be33c+dirty
protocolVersion: 1
schemaHash: sha256:5d7a9582b014e88f6787c41b577b467610abbfac23ffa3ce61d839fe2e315c48

bin/reasonix-linux-amd64-remote-bugfix
sha256 34c3b51c4a8be9794f2626fe4df64e24fe053dc54845cd00c5e9fbcc3a65d3c8

desktop/build/bin/reasonix-desktop-remote-bugfix.exe
sha256 40052acd2f5d66180209dd3c7aac2bc3d5dd4ca8776171ff1907f6dc066d907d
```

Wails 2.12.0 未跳过 bindings 生成，复用已通过 production build 的 frontend `dist`；最终源码上的
Windows application rebuild 在 4.687s 内完成。`go version -m` 已核对 Windows exe 的完整 ldflags。

当前 WSL Host 随后在原生 WSL namespace 中完成了真实 systemd user lifecycle 复验。安装
`dbus-user-session` 并恢复 user manager 后，全程只使用上述新 CLI 管理 Host，没有手工覆盖 managed
binary、手工启动 daemon 或绕过 unit：

```text
reasonix remote restart                     # PASS
reasonix remote status                      # PASS
reasonix remote doctor                      # healthy，11/11 检查通过
reasonix remote logs --lines 20             # PASS

reasonix remote stop                        # PASS
# enabled=yes, active=no, daemon unavailable, socket removed
reasonix remote start                       # PASS
# CLI/installed/daemon Build ID 再次完全一致

reasonix remote uninstall                   # PASS
# unit LoadState=not-found，managed binary 已移除
reasonix remote install                     # PASS
# enabled=yes, active=yes, socket 0600，doctor healthy
```

最终 managed binary 的 SHA256 为
`34c3b51c4a8be9794f2626fe4df64e24fe053dc54845cd00c5e9fbcc3a65d3c8`，与工作区 Linux
构建产物逐字节一致；CLI、installed、daemon 均报告上方同一完整 Build ID。服务最终保持 enabled、
active/running，lingering enabled。SSH 登录环境使用的 `/home/taibai/.local/bin/reasonix` 也已原子同步
为同一 SHA256 与 Build ID，避免远端 `reasonix remote attach --stdio` 在认证后因旧 CLI 版本被严格拒绝。

## 尚未完成内容

阶段 7 已完成当时环境内可执行的代码、协议、CLI、daemon、Desktop backend/frontend、RMT-046 与自动化
验收；阶段 8 补齐 Windows OpenSSH → WSL 的 config/public-key 实机闭环，阶段 9 又形成并部署 clean
`91fd2029` 协调候选。以下仅列截至阶段 9 仍需在对应候选或外部实机环境逐项执行并记录的验收，不得伪称
已经完成：

1. 当前 WSL Linux Host 已完成
   `restart → status/doctor → logs → stop/start → uninstall/install` 全链路；仍需在独立物理机或 VM 的
   普通 Linux 登录/SSH 会话中重复该流程，覆盖非 WSL 的 PAM、session bus 与 lingering 环境差异。
2. `reasonix-wsl` 的高级 alias/config、既有 known_hosts 与公钥认证已实机通过；仍需验证首次 Host Key、
   Host Key changed，以及密码、密钥口令和 keyboard-interactive/2FA 矩阵。direct/no-config 被用户本轮
   “使用现有 config”指令覆盖，不列为当前完成阻塞。
3. 阶段 8 的 V1 基本工作台路径、运行中 SSH 断线恢复、第二客户端 `HOST_BUSY` 与 Build ID mismatch 已
   实机通过；91fd 的跨 target projection 复验单列于下一项。仍需验证 pending Approval/Ask、Desktop
   重启和 daemon 崩溃重启等恢复矩阵。
4. clean `91fd2029` 已完成原生 Windows 启动、config 连接、Host capabilities 与 Linux 目录浏览；仍需在
   computer-use 可稳定激活窗口后完成 Remote → Local → 同一 Remote projection、创建进行中 close gate 与
   `/clear` 最终删除。无控制台已有 runtime probe 和本轮 GUI 观察证据，但单次观察不扩写成高速闪现测试。

## 真实阻塞

当前没有架构冻结项、代码或协调候选身份阻塞；clean `91fd2029` 已完成双端构建、部署与自动化门禁。
本目标剩余的当前环境门禁是 Windows GUI 自动化窗口无法稳定激活；`/clear` 本地删除另受动作时确认约束，
未获确认前不会执行，也不会用相邻测试冒充。系统默认
`/usr/bin/go` 为
Go 1.18.1，无法解析仓库的 Go 版本和 `toolchain` 指令；已在 `/tmp` 使用项目要求的 Go 1.26.5
工具链解决，不构成阻塞。

当前 WSL 最初缺少 `/run/user/1000/bus` 与 `dbus-user-session`；获得用户授权后已安装该系统包并恢复
`user@1000.service`。Codex 命令沙箱内直接执行 `systemctl --user` 仍会因 bwrap user namespace 的
inner/outer UID 映射在 private socket 认证阶段失败；相同命令在原生 WSL namespace 成功。这是执行
沙箱边界，不是 Host 或 `XDG_RUNTIME_DIR` 尾斜杠缺陷。已撤销针对该误判的实验性环境改写，产品代码
不携带 sandbox 特判，所有 lifecycle 证据均来自原生 WSL namespace。

当前已有 Windows OpenSSH → WSL 的 config 实机闭环；仍无外部物理 Linux 闭环。Windows GUI 自动化通道
本轮因窗口激活失败而不可用，Linux Wails 仍缺少上述原生开发包。自动化已经覆盖 CLI manager/probe、
精确 systemctl argv/顺序、真实 Unix socket 与生产 daemon、文件事务、SSH argv/AskPass/transport、
Windows 交叉编译及故障分支。剩余实机人工验收不能据此跳过，也不能伪称已经实机通过。

## 阶段 8：原生 Windows OpenSSH → WSL 收口（2026-07-18—2026-07-19，历史记录）

本节记录 `codex/remote-feature` 在 `89a55a5208d3e86e98ac237d7a49cd27e7c8eb28` 上启动的
原生 Windows 收口工作，并覆盖上方阶段 7 对“当前阻塞”和 Windows GUI 自动化条件的历史描述。
工作目录为 `D:\reasonix`；`site/package-lock.json`、`_go-learn/`、`developer-portal/` 未被修改、
清理或覆盖。

### Windows OpenSSH、既有 config 与 Host lifecycle 实机证据

- 实际 resolver 路径：`C:\Windows\System32\OpenSSH\ssh.exe`。
- 实际版本：`OpenSSH_for_Windows_9.5p2, LibreSSL 3.8.2`；原生测试已启动该绝对路径执行
  `ssh.exe -V`。
- `Test-NetConnection 127.0.0.1 -Port 22` 成功，证明 Windows 到 WSL sshd 的 TCP 路径可达。
- 初次未显式使用用户 config 的探测真实到达 sshd，但因没有命中已有认证配置而返回
  `Permission denied (publickey,password).`。用户随后明确要求查找并使用现有连接配置；只读取
  `C:\Users\ppoo2\.ssh\config` 的非秘密连接设置后，确认其中已有 alias `reasonix-wsl`。没有读取、
  输出或修改任何私钥正文、密码、口令或认证响应。
- 目标附件中的早期环境快照还把“Windows `.ssh` 只有 `known_hosts`、没有私钥、ssh-agent 不可用、
  Linux `authorized_keys` 不存在”列为已知状态；当前 config 已明确引用可工作的身份文件，且 BatchMode
  公钥认证成功，因此这些描述至少不能继续作为当前事实。没有时间戳证据可证明早期快照与当前状态之间
  何时变化，本节只记录可复验的当前结果；没有读取身份文件正文，也没有为追溯该推断而读取或修改
  `authorized_keys`。
- 以 `-F C:\Users\ppoo2\.ssh\config`、`BatchMode=yes`、`StrictHostKeyChecking=yes`、禁用 TTY、转发、
  local command 与 remote-command override 的安全参数连接 `reasonix-wsl`，真实
  `Windows ssh.exe → WSL sshd` 非交互认证 PASS。部署前远端 `reasonix version` 为
  `reasonix desktop-v1.17.12-264-g276a52fa`，证明先前失败是未使用既有 config，而不是仍需新增认证授权。
- clean `89a55a52` Linux CLI 先上传到唯一临时路径并在远端核对 SHA256；随后备份旧登录 CLI，并把
  已校验的临时文件原子替换为 `/home/taibai/.local/bin/reasonix`。managed binary 没有被手工复制或
  替换；部署后只通过新 CLI 执行 `reasonix remote restart`，再执行 `status` 与 `doctor`。
- lifecycle 实机结果为 restart PASS、服务 enabled/active，`doctor` 11/11 PASS；部署后
  `reasonix version` 为 `reasonix desktop-v1.17.13-10-g89a55a520`。登录 CLI 与 managed binary 的
  SHA256 均为 `146d981ddddfa9890efa6ed49998849712e429e2ee222bee1c686ff047033f56`，CLI、installed、daemon
  的完整 Build ID 均与下节冻结身份一致。

### 固定提交的协调构建

两个固定身份协调基线产物均来自同一个只读 `git archive` 干净快照，而不是当前工作树；Build ID 为：

```text
productVersion: desktop-v1.17.13-10-g89a55a520
sourceRevision: 89a55a5208d3e86e98ac237d7a49cd27e7c8eb28
protocolVersion: 1
schemaHash: sha256:5d7a9582b014e88f6787c41b577b467610abbfac23ffa3ce61d839fe2e315c48
```

```text
D:\reasonix\bin\reasonix-linux-amd64-89a55a52
sha256 146d981ddddfa9890efa6ed49998849712e429e2ee222bee1c686ff047033f56

D:\reasonix\desktop\build\bin\reasonix-desktop-89a55a52.exe
sha256 37313db0c2bc720c2c6efce8cfb849f2dbf78a234944ffd9cd32885abb9d7213
```

`go version -m` 核对了两个产物的 `linux/amd64`、`windows/amd64` 模块身份；二进制只读扫描也核对了
完整 productVersion、sourceRevision 与 schemaHash。上节实机部署及 lifecycle 已确认登录 PATH CLI、
managed binary、运行中 daemon 三方为该身份。固定 Desktop 的 SHA256 为
`37313db0c2bc720c2c6efce8cfb849f2dbf78a234944ffd9cd32885abb9d7213`。

### 本轮原生 Windows 自动化与实现收紧

- System OpenSSH resolver 原生测试验证默认解析结果为存在的 System32 绝对路径，并可执行 `-V`。
- Windows SSH transport 使用 `CREATE_SUSPENDED → Job Object assign → resume`，context cancel、live
  `Close` 与正常 `Wait` 均只有一个 Job handle 所有者。测试真实派生并持有后代进程句柄，逐一证明父、
  后代均退出且父进程被 Wait 回收；Job Object 无法建立时会在恢复 `ssh.exe` 前 fail closed，不再静默
  降级为只杀父进程。
- `TestRemoteSSHProcessHasNoConsoleAtRuntime` 以隐藏的 `CREATE_NEW_CONSOLE` 子进程作正控，原生
  `GetConsoleWindow` 返回非零；同一 probe 经生产 `newRemoteSSHProcess → CREATE_SUSPENDED → Job Object →
  resume` 路径启动时返回零。focused normal 与 race 均 PASS。这是当前工作树的原生进程级“没有分配
  console”证据，不属于 clean `89a55a52`，也不等价于 GUI 点击式“绝无闪窗”复验。
- Remote Session 创建 admission 与 final native close 在同一 mutex 下线性化；创建结果未知期间关闭被
  阻止，background hide 不能撤销另一条已建立的 final close，受阻的 force/system quit 标记不会污染
  后续普通关闭。
- 修正 Windows 上 Host 返回的 workspace 内绝对路径显示与安全 command 显示边界；外部绝对路径、
  Windows drive path、反斜杠和 shell 控制字符仍 fail closed。
- 两个旧 frontend 测试显式固定 `navigator.language=en-US`，避免 Node 26 在中文 Windows 上自动选择
  `zh-CN` 后用英文文案断言；pending-prompt 测试也会等待上一场景的 debounce 计时器排空。均为测试
  夹具确定性修复，没有修改产品语言或 prompt 行为。
- 新增 Windows-only、默认跳过的真实 SSH 集成测试。只有显式设置
  `REASONIX_REMOTE_SSH_INTEGRATION=1` 并提供 SSH executable、config、known_hosts 与 Host alias 的绝对路径后
  才会读取配置和发起连接；默认运行在检查这些值之前立即 SKIP。本次实机明确传入
  `C:\Windows\System32\OpenSSH\ssh.exe`；另有 resolver 测试独立核对默认值确为该 System32 路径。夹具先用
  protected DACL 且不共享 delete
  的目录 handle 固定临时路径，复制公开 known_hosts，并通过 wrapper `Include` 使用现有 config，同时强制
  BatchMode/publickey-only。IdentityFile 仍只由 OpenSSH 按现有 config 读取；测试不读取、复制或输出私钥。
- clean `89a55a52` 源码 archive 仅叠加上述测试夹具后，真实链路先严格拒绝错误 revision
  （`VERSION_MISMATCH`，无 fallback/recovery）；正确身份随后创建唯一 disposable Topic/Session、原子订阅并
  启动无模型 Shell。收到 before marker 后取消首个生产 SSH transport并保存 `LastSeq`；gap marker 在断线
  期间产生。重连保持同一 lease/HostEpoch、generation 递增，重订阅返回 `boundarySeq > saved LastSeq` 的
  atomic snapshot，其中保留 before/gap 与 active operation；之后事件严格从 `N+1` 连续推进至
  after/result/done。最后 close/trash/purge Session、delete Topic、detach，所有清理均 PASS；最终实机用例
  34.15s。输出不含 raw stderr 或不透明身份值。
- 该用例是 system OpenSSH + transport-neutral client/protocol 的机器证据；它显式调用 reconnect/subscribe，
  不把 Desktop workbench 自动恢复或 GUI tree 伪写成已由本用例断言。

最终文件上的本地门禁：

```text
cd D:\reasonix\desktop
go test -count=1 ./...                                      # PASS
go test -race -count=1 -run '<Remote/SSH/TargetManager>' .  # PASS

cd D:\reasonix
go test -count=1 <proc/ACP/rpcwire/RuntimeAPI/Remote packages>  # PASS
go test -race -count=1 <同上 Remote 相关 packages>              # PASS
go run ./cmd/remote-protocol-gen -check                         # PASS

cd desktop/frontend
pnpm typecheck && pnpm test:typecheck && pnpm test && pnpm build # PASS
```

最终 Desktop 全量复跑完成并 PASS（主包 284.467s）。此前一次复跑曾在 Windows 系统
`CreateProcess` 内出现原生 access violation，没有 Go 测试断言失败；随后 System OpenSSH resolver 与
cancel/live Close/normal Wait 三条真实 Job Object 用例连续 20 轮全部通过，带最近测试跟踪的主包全量也
越过原崩溃点完成。该单次系统异常未复现，不作为产品测试通过的替代证据。

Wails 2.12.0 在最终工作树上重新生成 bindings 并完成 Windows amd64 application compile；另有 Linux
amd64 CLI 交叉编译用于证明当前改动的双平台可编译性。两者使用明确的 `+dirty` 验证身份并单独命名，
不会冒充或覆盖上方固定 `89a55a52` 的协调产物，也不会用于严格 Build ID 实机闭环。

在加入 OpenSSH `HideWindow` 修复后，又以不带 `-clean`/`-nsis` 的直接 Wails 构建生成唯一验证文件
`D:\reasonix\desktop\build\bin\reasonix-desktop-89a55a52-dirty-hidewindow.exe`；其 SHA256 为
`bf5b4d66953c2df25f819da199a32bb0cd81d8ff31cb20ca027c7dc93d8dc2e0`。其 Build ID 为
`desktop-v1.17.13-10-g89a55a520+dirty`、
`89a55a5208d3e86e98ac237d7a49cd27e7c8eb28+dirty`、protocol `1`、
`sha256:5d7a9582b014e88f6787c41b577b467610abbfac23ffa3ce61d839fe2e315c48`；`go version -m` 还核对了
`-H windowsgui`。该构建证明当前源码可形成 Windows GUI 产物，但不能替代上方固定基线；本轮 Computer
Use 通道连续两次无法激活既有 Reasonix 窗口（`failed to activate captured window`），按自动化安全规范
停止输入，因此尚未取得该 dirty 构建的原生点击式“无控制台闪窗”复验证据。

### 固定 Desktop 原生 GUI 与真实 SSH 闭环

- 旧 Desktop 进程 PID 23924 已先终止，再启动上方 SHA256 固定的 clean `89a55a52` Desktop。Host 保存为
  config 模式，alias 为 `reasonix-wsl` 并显式使用现有 config；连接成功。实际子进程为
  `C:\Windows\System32\OpenSSH\ssh.exe`，真实链路为
  `ssh.exe → sshd → reasonix remote attach --stdio → daemon`。可见连接生命周期日志保持结构化，不含
  SSH argv、raw stderr、私钥路径或认证秘密。
- Host 目录浏览器真实展示 `/home/taibai`，并成功创建 topic `Remote V1 config loop 2026-07-18`。首次创建后单个
  Session 折叠为一个父项；执行 `/new` 后显示同一父项和两个 Session 子项。模型显示为
  `deepseek-v4-pro`，没有再把 `Topic` 误显示为模型。
- 手工终止 GUI 的子 `ssh.exe` 后，daemon 的 `MainPID=3673786` 与
  `ActiveEnterTimestampMonotonic=115161621990` 均未改变，证明 transport 断开没有重启或杀死 Host
  daemon。GUI 自动重连后恢复同一个 topic、Session tree 与 `deepseek-v4-pro` 模型。
- GUI 持有 lease 时，另一个由 clean `89a55a52` 客户端发起的真实连接收到 `HOST_BUSY`。随后上述 opt-in
  自动化在 GUI 释放 lease 后独立完成真实断线与同 lease/HostEpoch 恢复，并机器断言断线期间 seq gap、
  atomic snapshot 与 snapshot 后连续 `N+1`；GUI 则实机观察到重连后同一 topic、Session tree 与模型恢复。
  两类证据互补，但 opt-in 测试显式调用 client reconnect/resubscribe，没有直接断言 Desktop workbench 的
  自动恢复或 GUI tree。
- `/clear` 已走到最终本地 transcript 删除确认框；该删除动作需要发生时的用户明确确认，当前未获确认，
  因此已取消且不能记作完成。此后 dirty GUI 复验又因 Computer Use 无法激活窗口而停止，两者是不同边界。
  固定 clean Desktop 的原生运行还观察到 OpenSSH 控制台窗口意外弹出；当前工作树已设置 `HideWindow`，
  并以有正控的 Windows runtime probe 证明生产 Job Object 路径未分配 console。该修复尚未进入固定
  `89a55a52` 基线，故不能把固定基线写成已复验通过。
- 目标原文要求 direct/no-config 路径；用户本轮随后明确要求“找到并使用”已有 config，故本节按该最新
  明确指令验证 config 模式。该覆盖不等于 direct 模式也已实机通过。

### 阶段 8 时点的候选身份冲突与未完成矩阵（历史）

以下是阶段 8 结束时的历史判断：对已验证的 `reasonix-wsl` config/public-key 路径，SSH 认证授权已不再是
外部前置；当时仍存在 clean `89a55a52` 不含 Windows Job Object、native close gate、Windows path 与
`HideWindow` 修复，而验证产物只能诚实标记 `+dirty` 的候选冲突。该冲突后来由用户授权的本地提交
`0167f7f39` 与 `91fd20297`、以及阶段 9 的 clean 双端协调构建彻底解决；本段不得再解释为当前阻塞。

以下结果仍保持“未完成/未通过”，不能由 mock、`net.Pipe` 或相邻证据替代：

1. `/clear` 最终确认与删除结果，以及创建进行中关闭保护的固定候选 GUI 实机复验。
2. OpenSSH 控制台窗口修复进入一个 clean 协调候选后的原生 GUI 复验；当前已有工作树实现及 normal/race
   原生 runtime handle 证明，但固定 `89a55a52` Desktop 仍会弹窗。
3. Desktop workbench 自动恢复与 GUI tree 的机器级联动断言，以及 pending Approval/Ask、Desktop restart、
   daemon crash、密码、密钥口令、keyboard-interactive/2FA、Host Key changed 和外部物理 Linux 人工矩阵。
   真实 seq gap/snapshot/`N+1` 与 Build ID mismatch 已由 opt-in 自动化 PASS，不再列为未完成。
4. direct/no-config 模式的原生 Windows 实机闭环；本轮用户明确覆盖为已有 config 模式，只完成了后者。

## 阶段 9：clean `91fd2029` 协调候选（2026-07-20，当前权威状态）

本节覆盖阶段 8 的“89a clean 与 dirty 工作树候选冲突”结论。用户已明确允许建立双端和本地提交；实现
先形成 `0167f7f39277803112273343e50b4b529dda30c9`（Windows lifecycle、Remote workbench 与真实 SSH
夹具），再形成 `91fd202971cdde5b793d674a8699f25938ac521b`（跨 target authority 的 backend reattach
顺序与 frontend projection/hydration 隔离）。两个提交都只保存在本地分支，没有 push、PR 或发布。

### 当前协调身份与双端产物

Desktop、SSH 登录 CLI、managed binary 与运行中 daemon 使用同一 Build ID：

```text
productVersion: desktop-v1.17.13-12-g91fd20297
sourceRevision: 91fd202971cdde5b793d674a8699f25938ac521b
protocolVersion: 1
schemaHash: sha256:5d7a9582b014e88f6787c41b577b467610abbfac23ffa3ce61d839fe2e315c48
```

```text
D:\reasonix\desktop\build\bin\reasonix-desktop-91fd202971cdde5b793d674a8699f25938ac521b.exe
size 53138944 bytes
sha256 e7cade050299100cb8b0be48c12fcc1121289de053518ba71e5d6470b441c6ac

/home/taibai/DeepSeek-Reasonix/bin/reasonix-linux-amd64-91fd202971cdde5b793d674a8699f25938ac521b
size 30982306 bytes
sha256 b85617a7c8e25bd83171cf2073785a4a5bd434893a8908abbecc11a1cb2b8fe0
```

Windows 产物是 PE32+ GUI subsystem，Go 1.26.5、Wails 2.12.0、windows/amd64、CGO disabled、
trimpath；Linux 产物是静态、stripped、x86-64 ELF，Go 1.26.5、CGO disabled、trimpath，VCS revision
精确为 `91fd202971cdde5b793d674a8699f25938ac521b` 且 `vcs.modified=false`。两个产物的版本、revision 与
冻结 schema 均已通过内嵌字符串、
`go version -m` 或 CLI `version/status` 交叉核对。

### WSL lifecycle 部署与保护边界

- `reasonix-wsl` 对应仓库 `/home/taibai/DeepSeek-Reasonix` 以 fast-forward 到达同一 `91fd2029`；部署前后
  Git 状态均只含必须保护的 `site/package-lock.json`、`_go-learn/`、`developer-portal/`，状态摘要与
  lockfile SHA256 前后不变。
- 旧 `0167f7f39` 登录 CLI 已备份为
  `/home/taibai/.local/bin/reasonix.backup-0167f7f39277803112273343e50b4b529dda30c9-pre91fd202971cd`。
  新 CLI 经同目录临时文件、version/hash 校验、fsync 与原子 rename 安装；没有手工替换运行进程或 managed
  binary。
- 只通过新 CLI 执行 `reasonix remote restart`，结果 `completed`。稳定后登录 CLI、
  `/home/taibai/.reasonix/remote/bin/reasonix` 和 `/proc/<MainPID>/exe` 三者 SHA256 都是上方 Linux SHA，
  CLI/installed/daemon Build ID 完全一致。
- `reasonix remote status --json` 无 diagnostics；`reasonix remote doctor` healthy，11/11 PASS。systemd user
  service enabled、active/running、Result=success；socket `/run/user/1000/reasonix/remote.sock` 为 uid 1000、
  mode `0600`。

### 当前提交上的自动化门禁

- `remote-protocol-gen -check` PASS；Windows Wails application build 与 Linux static CLI build PASS。
- frontend `typecheck`、`test:typecheck`、完整测试集和 production build PASS；新增的 target authority
  regression 41/41 PASS。该回归覆盖旧 Local 异步 hydration、history/reconcile/poll/RAF 回调、相同
  tab/session id 碰撞、ContextPanel cache 与 `/new`/`/clear` projection generation 等边界。
- backend reattach 顺序与 target authority 定向集普通测试 `count=20` PASS，Windows race `count=10` PASS；
  覆盖 open session 独立恢复、ordered ready、旧 generation 不得发布 ready、新 target 赢得提交及 scoped
  reconnect failure。
- `D:\reasonix\desktop` 上 `go test -count=1 -timeout 20m ./...` 在原生权限环境全部 PASS：主包
  284.545s，`cmd/sign`、`cmd/update-helper`、`cmd/windows-resource`、`internal/update` 也全部 PASS。同一命令
  先在 sandbox 内仅因用户 Temp `Access is denied` 与 lease lock 文件权限失败；原生命令没有测试断言失败。
- 当前 HEAD 的 `internal/acp`、`internal/rpcwire`、`internal/runtimeapi`、`internal/runtimeservice` 普通测试
  全部 PASS；使用现有 UCRT64 GCC 临时启用 CGO 后，四包 Windows race 也全部 PASS。普通/race 总耗时分别
  40.645s、53.815s。
- `git diff --check` PASS。`desktop/window_controls.go` 仍显示 phantom `.M`，但 index、HEAD 与工作树 blob
  相同且 raw diff 为空；没有对它执行格式化、覆盖或广域 staging。

### 真实 config-backed OpenSSH 闭环

- 继续使用 `C:\Windows\System32\OpenSSH\ssh.exe`（OpenSSH_for_Windows_9.5p2）和既有
  `C:\Users\ppoo2\.ssh\config` alias `reasonix-wsl`；安全解析结果为 `taibai@127.0.0.1:22`、
  `IdentitiesOnly=yes`。IdentityFile 只由 OpenSSH 自行读取，测试与记录均不读取、复制或输出私钥正文。
- 当前 `91fd2029` 上 opt-in `TestRemoteSSHConfigAttachReconnectIntegration` 真实执行
  `System32 ssh.exe → WSL sshd → reasonix remote attach --stdio → daemon`，39.150s PASS。用例先断言错误
  revision 被 `VERSION_MISMATCH` 严格拒绝，再验证正确 initialize、disposable Topic/Session、无模型 Shell、
  断线期间 seq gap、同 lease/HostEpoch 且 generation 递增的重连、atomic snapshot、snapshot 后连续 `N+1`
  以及最终清理。
- 首次重跑误把测试包 linker symbol 写成 `main.version`，正确 daemon 因测试 peer 未得到预期身份而严格返回
  `VERSION_MISMATCH`；改用实际 test package symbol `reasonix/desktop.version` 后即得到上述 PASS。这是夹具
  注入错误及 fail-closed 证据，不是产品 mismatch 缺陷，也没有使用 force、降级或 fallback。

### 原生 `91fd2029` GUI 已证实与剩余边界

- 启动前没有旧 `reasonix-desktop` 进程；原生启动的唯一窗口来自上方精确 SHA 的 91fd executable。
- Remote Hosts 中选中既有 config 条目 `reasonix-wsl` 后成功到达 `Remote connected`。Windows 只读进程
  核对显示当前子进程为 `C:\Windows\System32\OpenSSH\ssh.exe`，其父进程就是该 91fd Desktop；没有读取
  SSH argv。连接生命周期 UI 只展示结构化 target 状态，未显示 raw stderr、凭据或私钥路径。
- GUI 展示 Host 报告的 Remote V1 capabilities，并由 Linux directory browser 返回 `/home/taibai` 与
  `/home/taibai/DeepSeek-Reasonix`；观察期间没有出现独立 OpenSSH console。另有带正控的 Windows runtime
  probe 证明生产 SSH Job Object 路径没有分配 console，但单次 GUI 观察不能扩写成高速摄像式“绝无闪现”。
- 在准备附加 Session 并复跑导致 `91fd2029` 的 Remote → Local → 同一 Remote 场景时，computer-use 对当前
  窗口连续两次返回 `failed to activate captured window`。按自动化安全规范已停止继续输入。因此当前候选的
  header、transcript、ContextPanel metrics、模型和 Session tree 在该转换后同时归属 Remote 的原生点击式
  复验仍未完成；41/41 frontend regression 与 backend ordered-ready 测试是强机器证据，但不冒充该 GUI
  结果。
- `/clear` 最终本地 transcript 删除仍未执行，因为破坏性 UI 动作需要动作发生时的用户确认。pending
  Approval/Ask、Desktop/daemon crash 恢复、密码、密钥口令、keyboard-interactive/2FA、Host Key changed
  和外部物理 Linux 继续列为人工矩阵；这些不由 config-backed WSL 闭环或 mock 代替。
