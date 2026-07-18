# Reasonix Remote V1 实现状态

> 冻结基线：[`REMOTE_ARCHITECTURE.zh-CN.md`](./REMOTE_ARCHITECTURE.zh-CN.md)
>
> 最后更新：2026-07-18
>
> 状态原则：只记录已经落地并由测试证明的内容；计划、接口草案和未运行的测试不计为完成。

## 当前阶段

阶段 7：当前环境可执行的 Remote V1 实现与自动化验收已完成。RMT-046 也已完成：Host 条目使用
`mode=direct/config`；默认 direct 保存 `destination=username@host` 与独立 port，config 保存
alias/sshConfigPath，v1 store 旧条目迁移为 config 并继续兼容。真实 Windows Desktop → 普通 Linux
Host 与外部 SSH 实机验收仍待执行；当前 WSL systemd user service 已完成本页记录的真实 lifecycle
复验，但不能替代普通物理机或 VM Linux Host 的验收。2026-07-18
收到的模型标签、Session 显示/新增可见性和 Remote 新建项目误开本地 picker 三项缺陷已经完成真实
修复、完整 Desktop/frontend 回归、变更面 race/E2E 以及协调跨平台编译，证据记录在阶段 7 缺陷修复
复验小节。

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

当前环境可执行的代码、协议、CLI、daemon、Desktop backend/frontend、RMT-046 与自动化验收已完成。
以下仅为必须在对应外部实机环境逐项执行并记录的人工验收，不得伪称已经完成：

1. 当前 WSL Linux Host 已完成
   `restart → status/doctor → logs → stop/start → uninstall/install` 全链路；仍需在独立物理机或 VM 的
   普通 Linux 登录/SSH 会话中重复该流程，覆盖非 WSL 的 PAM、session bus 与 lingering 环境差异。
2. 在 Windows Desktop 与外部 Linux Host 上使用同一提交协调构建，分别验证直接
   `username@host` + port 与高级 alias/config、known_hosts、首次 Host Key、密码、密钥、密钥口令、
   keyboard-interactive/2FA，以及 Host Key changed fail closed。
3. 在该实机闭环逐项走 V1 工作台能力，并验证运行中 SSH 断线重连、pending Approval/Ask 恢复、
   Desktop 重启、daemon 崩溃重启、第二客户端 `HOST_BUSY` 和 Build ID mismatch。
4. 在原生 Windows 上启动已生成的 Wails 应用并完成人工交互验收。

## 真实阻塞

当前没有架构、产品或代码阻塞；RMT-046 已实现并通过当前环境自动化门禁，协调构建也已部署到
当前 WSL Host 并完成上述 lifecycle 复验。系统默认 `/usr/bin/go` 为
Go 1.18.1，无法解析仓库的 Go 版本和 `toolchain` 指令；已在 `/tmp` 使用项目要求的 Go 1.26.5
工具链解决，不构成阻塞。

当前 WSL 最初缺少 `/run/user/1000/bus` 与 `dbus-user-session`；获得用户授权后已安装该系统包并恢复
`user@1000.service`。Codex 命令沙箱内直接执行 `systemctl --user` 仍会因 bwrap user namespace 的
inner/outer UID 映射在 private socket 认证阶段失败；相同命令在原生 WSL namespace 成功。这是执行
沙箱边界，不是 Host 或 `XDG_RUNTIME_DIR` 尾斜杠缺陷。已撤销针对该误判的实验性环境改写，产品代码
不携带 sandbox 特判，所有 lifecycle 证据均来自原生 WSL namespace。

另外两项环境边界保持不变：当前任务没有可靠的 Windows GUI 点击自动化通道/外部 SSH 实机闭环；
Linux Wails 缺少上述原生开发包。自动化已经覆盖 CLI manager/probe、精确 systemctl argv/顺序、真实
Unix socket 与生产 daemon、文件事务、SSH argv/AskPass/transport、Windows 交叉编译及故障分支。
剩余实机人工验收不能据此跳过，也不能伪称已经实机通过。
