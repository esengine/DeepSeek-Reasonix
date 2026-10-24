---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-22
---

# Concurrent mutation protocol / 并发变更协议

[Fleet coordination / 批量任务协调](CONCURRENT_MUTATIONS_FLEET.md)

## 中文

可选的跨平台受控验证入口见[受控容器验证](CONTROLLED_CONTAINER_VERIFICATION.md)。

本文记录已实现的协议及本地验收边界，详见[验收记录](CONCURRENT_MUTATIONS_ACCEPTANCE.md)。
新安装默认 `auto`；已有配置缺少策略字段时继续使用兼容模式，不改写旧开关。
`auto`、`shared` 和 `isolated` 覆盖 CLI、serve、ACP 的首次执行及持久环境恢复。
托管 CI、上游合并和发布是独立动作。

固定负载的协调器层对照及 P95 回退见[性能证据](CONCURRENT_MUTATIONS_PERFORMANCE.md)。
这份记录不替代完整产品性能验收。
真实装配、工具与进程对照见[产品并发测量](CONCURRENT_MUTATIONS_PRODUCT_PERFORMANCE.md)。

### 权限与事务

- `CapabilityGrant` 决定允许写哪里；任务分工不再代替执行互斥。
- `MutationImpact` 来自实际工具契约，独立于权限与验证证据分类；测试命令也可能
  生成文件，因此 Shell 保守协调工作区。额外元数据资源不扩大写入授权。
- 新版路径只在实际变更、必要 hooks 和变更观察期间持有事务。模型推理不保留
  上次文件写入的执行租约。
- 真实工具解析、常规审批和路径扩权先完成，再一次申请完整资源集合。
- 重叠资源按入队顺序获得机会，不相交的资源可以绕过等待者。
- 文件资源等待最多 30 秒；超时返回可重调度错误，不夺取仍有效的资源。
- 跨进程注册临界区很短，工具执行不持有注册表锁。每个事务另有 OS 活性锁，
  进程退出后才能回收已知格式的遗留记录。
- Git 身份解析与旧 claim 登记在注册临界区外完成。独立的准备 OS 锁保留队列
  优先级，避免本事务已取得的兼容 claim 被误认为旧进程阻塞而让后来者插队。
- 旧运行时结束回调只释放其唯一事务 ID 对应的资源。未知版本或损坏的记录
  保留并拒绝继续协调，不能靠删除文件恢复运行。

父代理和每个子代理分别记录实际读取的文件版本。全文件覆盖需要读过该文件，
并且当前内容仍匹配该代理的记录。读文件工具使用同一次读取的字节计算记录，
不再次打开文件生成另一个版本。大于记录上限的读取不会产生可用于覆盖的版本。

审批后会在锁内重算变更预览；发现版本变化则返回冲突，更新预览后重新调度。
hooks 改变预览内容也会阻止继续应用。编辑器缓冲区需要实现版本比较写入；
不支持此能力的活动缓冲区会明确拒绝安全写入，不静默回落磁盘。读取未保存
缓冲区仍受读取权限约束，磁盘上不存在文件不代表可以读取它的缓冲区。

子代理开始、结束 hooks 分别使用短事务，不把代理推理包在 hook 锁中。
取消后尚未确认结束的进程继续保留物理租约，但旧调用立即失去执行权。

等待事件携带事务、执行、会话、工具调用及持有者身份，结束事件沿用同一事务 ID。
取消、超时、版本冲突和准入拒绝分别记录。Desktop 与 TUI 按事务配对并幂等重放，
不能关闭另一子代理的等待；旧日志没有事务 ID 时仅匹配旧格式等待。

宿主暂存、提交、丢弃及分支切换也进入协调器。提交消息生成前释放 Git 快照事务；
用户 `!command` 在进程实际结束后释放资源，再进入模型回答。丢弃在排队前记录
文件、index 和 HEAD，等待后任一变化都会返回冲突。

不明确范围的 Bash/hooks 保守包含已识别主工作根与额外根的 GitDir/CommonDir。
权限分类中的“只读命令”不代替工具影响声明，例如 Git 的只读查询仍可能更新缓存。
这不增加工具写权限，也不声称覆盖脚本随意指定的所有外部仓库。

Shell 获得资源后重新解析 GitDir、CommonDir 和 WorkTree；排队期间 `.git` 或
`commondir` 指针改变会拒绝执行。固定身份的宿主 Git 命令仍使用其固定环境。
推送只在准备及更新本地跟踪引用时持有源仓库事务。网络传输使用已复制对象的
独立临时仓库；远端成功后本地引用变化不会被覆盖，界面明确提示无需重复推送。

### 整合日志与恢复

`Apply` 和 `ApplyPaths` 共用整合实现。前者另要求整体工作副本仍匹配原快照；
后者按路径进行基线/目标比较。比较包含 Git 模式、内容和文件类型。

整合日志位于该工作副本 Git 管理目录的 `reasonix-apply-v1/active.json`，
不进入用户索引。写入前持久化各文件的原始前像和目标像，再逐个原子替换。
普通文件系统不提供跨文件原子提交，观察者可能看到应用中的中间状态。

中断恢复只恢复仍等于事务写后版本的文件；原样未执行的文件不动。检测到外部
修改时保留该文件和日志，返回恢复状态。重试整合先处理遗留日志，再重新判断基线。

当前程序的所有策略遇到未完成日志都会拒绝普通写入，包括仍有活性所有者的
日志；切换兼容策略不能绕过恢复。协调器内排队仍使用独立的活性判断。
恢复工具必须申请日志涉及的全部资源，不能只持有新候选的变更路径。

工具权限、观察和协调资源都从同一最终写根派生，包含显式 `sandbox.allow_write`
和覆盖的工作根；`WorkspaceOnly` 只保留隔离现场。重建复用旧 workspace owner 时
合并各运行时的恢复屏障，保留仍可能有后台写入的旧资源域。

宿主还会在取得日志锁后重新检查有效租约范围；没有恢复授权时拒绝自动回滚。
新版策略提供 `recover_integration`，无需先创建另一份隔离候选即可处理日志。

隔离结果在 Git 创建 worktree 前记录准备中状态、身份和运行 generation。
准备中、运行中、待整合、整合中、已整合、中断、失败、取消、冲突和恢复待处理分别表示。

准备期间退出会保留记录及部分环境，重启后明确要求恢复，不自动重跑 reset。
可显式丢弃已注册的候选；无法验证归属的未注册残留目录保持原样。

运行或整合失败、清理失败和异常退出不能冒充成功。
有未整合修改的结果不会按 14 天直接删除；仅证明没有修改的闲置结果可过期清理。
同一结果的应用与丢弃使用同一个跨进程操作锁。

结果操作先读取 manifest、申请完整资源，再取得结果锁并重新检查代际和内容。
等待文件或 Git 资源时不持有结果锁；完成候选清单前也要等待残留写进程清理。

新结果写入 `entry-v2.json`，旧版扫描 `entry.json` 时无法发现它们。
已有旧 marker 继续读取，活动候选不迁移。完成应用或丢弃时才复制完整终态记录到
新 marker，确认原始字段相同后删除旧 marker，再清理 worktree。未知字段保留。
已经加载旧记录的旧进程仍可能执行旧清理逻辑，这是旧程序并存的保障边界。

清理后的 `entry-v2.json` 是持久完成记录：保留 `iso_`、基线、清单、证据和最终决定。
清理完成单独记录；进程在删除目录后退出，可以重试同一操作完成清理，不重新应用
源文件。相反决定被拒绝。显式丢弃内容已漂移的候选时，保留原封存清单作为历史。

所有控制器直接从磁盘读取待处理状态，不从本地缓存复活已完成结果。ID 分配标记
防止结果 ID 重用；损坏的身份记录保留并阻止新分配，需要明确恢复处理。

新版候选完成记录包含内容摘要，读取实际字节、类型和模式，也覆盖忽略文件。
读取上限、缺失输入、外部符号链接或嵌套仓库使观察不完整且不生成摘要。

用户配置中的 `agent.isolation_copy_paths = ["deps"]` 声明要独立复制的仓库相对路径，
项目配置不能扩大这个列表。默认不复制忽略目录。普通文件与空目录保留模式；
符号链接、特殊文件、敏感文件名、Git 元数据、越界及禁读范围会拒绝准备。

准备期间联合协调源、候选和 Git 元数据，读取实际字节并检查源与目标。
复制的内容不通过硬链接或可写目录链接连回主工作区。准备凭据包含声明和内容摘要，
不表示外部环境完整，也不表示任务验证通过。

Git 内容快照另存版本化的普通权限位记录，物化时恢复捕获的文件、父目录与根目录
权限，不依赖 checkout 进程的 umask 或源文件之后的 chmod。gitlink 只保留目录，
不复制子模块内部内容。

记录必须完整覆盖对应类型的树条目，写入和读取同受 8 MiB
限制；读取前先检查对象大小。候选 HEAD 的额外父提交保留该记录，首个父提交仍为
原 HEAD，源 refs 与 index 不变。这不表示复制 ACL、扩展属性或特殊权限位。

初始内容基线覆盖候选的整个支持域。忽略文件即使在声明目录外新增，也阻止自动
清理。应用及整合在锁内拒绝 Git 清单之外的变化，保留原成果供继续处理或显式丢弃。
缺少完整基线的旧结果继续读取并保留；无法证明其额外内容安全时不自动应用或清理。

`workspace-content-v1` 明确不包含根 Git 元数据及外部环境；它只绑定候选结束时
的内容，不能证明这些内容已通过测试。候选验证状态仍为未知，完整验证及自动整合
必须另外绑定检查输入、Git 状态、环境与提交时重新校验。

隔离任务会在模型执行结束后，用宿主冻结的 `verify:` 命令再次检查候选。
宿主执行器使用独立会话与证据，经过普通工具权限、路径约束、hooks、沙箱和事务；
不采样模型，也不恢复原任务禁用的 Bash。扩展替换检查命令时会拒绝执行。

检查必须拥有可信的 `passed` 结果，单独的退出码 0 不足以通过。
每项检查具有独立执行身份，不能继承源任务的证据或重复累计前项检查。
检查阶段取消与异常仍封存候选并释放运行占用；完整验证证明尚未建立前，结果继续
保留为未知，不自动显示主工作区已验证。

Linux 可在用户配置中显式声明 `agent.verification_runtime` 离线根文件系统。
宿主冻结源码与运行文件，使用只读挂载、独立 PID/网络空间及私有临时目录；不继承
宿主环境变量。启动握手固定 PID 1 的身份，取消及完成均等待真实执行域终止。

缺失执行域完成证明时保留资源，不用监控进程退出冒充完成。凭据由当前进程拥有，
JSON 标志不能伪造；重启后须重新验证。

各宿主也可显式选择[本地容器验证](CONTROLLED_CONTAINER_VERIFICATION.md)，
通过 Unix socket 或 Windows 命名管道运行不可变 Linux 镜像。
普通 Shell 观察继续显示未知，不会因此获得自动整合权限。

最终进程启动边界记录实际 argv/环境摘要、命令摘要、解释器内容身份、目录、平台、
执行前后内容与物理 wait 完成情况。参数与环境原值不落盘。新执行代次清空上一次
的当前检查；准备失败、取消和旧回调不能继承旧检查成功。

宿主检查成功后，会从当前源工作副本建立独立 `int_` 整合尝试，应用原候选差量并
重新检查。记录位于 `.integration-v1`；原 `iso_` 基线和封存清单保持不变。
检查期间不持有源文件事务。准备使用同一根任务额度，异常释放准备占用。

自动提交要求完整有效凭据，并在资源锁内重验原结果代次、源内容/Git 输入、候选
内容、原源会话及写入授权。整树协调范围不等于恢复写权限；日志锁内逐次授权
实际恢复路径。先比较完整预计发布结果，再写文件；忽略依赖、空目录及模式差异
不能等写入后才发现。日志保留结果 ID 与整合尝试 ID。

Linux 受控运行时提供完整有效凭据时，生产路径会自动检查并整合；提交前重新核验
源版本及授权。普通启动观察仍保留 Git/外部环境覆盖缺口，保持 `unknown` 并保留
两份候选。测试中的覆盖替身只验证提交状态机，不替代真实受控运行时验收。
显式丢弃整合尝试不会删除原 `iso_` 结果。

### 兼容矩阵

新版策略下，`task` 的 `isolation` 接受 `auto`、`shared`、`worktree`。
显式 worktree 任务已通过与共享任务相同的 profile、工具与路径限制、model/effort、
步数、调度和后台执行路径。

新版子代理不再整段占用执行名额：模型请求、变更和
准备分别使用根调度器拥有的独立额度。文件与变更额度一次申请；路径阻塞不占额度。
大额度请求不会被后来的小请求饿死，不相交额度池仍可继续。

模型额度从请求发出前持有到底层 provider 流关闭。取消先结束消费者等待，再等待
源流真正关闭后释放；重试退避不占额度。辅助模型请求与主推理共用这一入口，
不包装 provider，不改变可选能力识别和请求字节。

远程 broker 的本地流关闭不构成
远端模型已退出的证明；远端宿主额度仍待实现。此处的根预算约束并发资源，
既有花费记账不被解释为跨代理汇总。

Git 快照及 worktree 创建将准备额度与相关路径联合申请；内核装配与子代理注册、
记录准备使用各自短准备阶段。准备队列取消不创建候选；已经持久化准备记录后的
失败保留恢复信息。运行与等待阶段不继续占准备额度。

每个根调度器最多保留 `max(64, max_subagent_concurrency)` 个已接纳子代理，包括
等待模型的任务。超额在创建候选或后台作业前返回 `execution.resident_limit`，
可先收集或取消已有任务再重试；嵌套任务不排队等待自己的父任务退出。
这项驻留上限不等于模型并发数，也不限制已完成且保留的隔离结果数量。

同会话运行时重建及隔离内核复用根额度身份和当前限额，不能借重建获得第二份
额度。存活可写任务计数独立于短期执行额度。现有 `max_subagent_concurrency` 和
`max_parallel_writers` 仍只约束子代理，不把根代理主请求纳入同一额度池。

设置接口和桌面分别展示保存的协作策略与当前聊天的实际策略、子代理并发和写入额度。
有效值来自已装配的 owner，重建保留旧额度时不会显示配置中的新额度。保存不会切换
活动运行。

显式 Home 的设置写入绑定该 Home 的用户配置，严格读取后按用户作用域
持锁更新；原有乐观模式与等待值保留在兼容设置中。

新聊天在首次执行前按实际工作副本的跨进程执行登记选择隔离；已开始的聊天不自动迁移。

隔离准备不支持的角色组合会明确拒绝；自动模式回退共享执行时，保留同一角色、
工具范围和会话绑定。后台仅传递声明的身份与偏好，不继承来源会话的审批器和
后台启动能力；子运行使用自己的宿主服务。

新版只读 skill 也使用统一执行入口，保留临时、不持久化子会话与原审查权限边界。
完成协议工具在创建运行时确定；相同调用的系统及工具前缀保持稳定。

后台 Shell：

- 实际启动进程前申请独立事务，进程启动后即释放；排队取消不会启动命令。
- 启动后的进程按外部写入者对待，不占用工作区，长期运行的服务不阻塞其他写入。
- 具名文件的写入仍由版本检查发现它造成的变化。
新版协议拒绝显式脱离任务的 Shell 进程；服务请直接使用 `run_in_background`。
协议不能承诺约束任意自行脱离的外部程序。

隔离会话记录携带环境 ID 与来源工作区。原聊天使用 `continue_from` 可以在保留
的候选中继续，续跑、应用和丢弃互斥。无修改但可续跑的环境也会保留；过期的
无修改环境仍可能清理。每次运行记录新的 generation，旧回调不能结束或删除
新运行。恢复中断会话要求同一环境的新 generation，仍检查 persona、工具和模型。

根聊天 fork 独立重开，祖先 task 的 `continue_from` 创建并复用当前聊天自己的副本。
中断执行保留文件，续跑要求新的 generation；未形成可执行现场的准备记录进入恢复。
详见[续跑规则](CONCURRENT_MUTATIONS_CONTINUATION.md)。

具有宿主环境工厂的新版运行时支持嵌套
worktree，目标始终是直接父环境；共享及只读选择不变。登记、授权、保留和恢复
规则见[嵌套成果说明](CONCURRENT_MUTATIONS_NESTED.md)。

隔离写入任务的每次执行拥有独立的后台作业管理器及执行身份；不会借用父任务或
兄弟任务的作业表。工具白名单允许时，可以使用后台 Bash、`wait`、`bash_output`
和 `kill_shell`。只读子任务仍无后台能力。父任务返回前原子关闭新作业准入，等待
所有已登记作业及受跟踪的进程等待完成。

随后再释放临时目录、验证和封存候选。
取消会停止所属作业，但不能用取消返回替代真实退出；未确认的进程等待继续保留
环境。此规则不承诺约束任意自行脱离的外部进程。

后台作业收尾后，证据先进入实际执行代理的账本，再生成完成说明。无法建立
先后关系的并发检查不能证明这些修改已验证；需要在收齐修改后重新运行。

后台结果收集只导入变更观察；没有共同快照身份的命令、读取和评审证明保留在
原作业记录中，不成为收集方当前版本的证明。`wait`、`bash_output`、回合重租
和执行收尾使用同一规则。

完成报告拒绝引用最后一次修改之前的检查，交接文本显示同一命令的最新结果，
并标记被后续修改使其失效的通过记录。

前台和后台隔离执行使用候选独立证据账本，不能把候选检查合并为源工作区检查。
候选记录保留工具、来源执行、路径、退出状态及输出摘要，不持久化原始参数和
命令文本。当前这些观察的验证状态为 `unknown`，不能授权自动整合。

### 保留成果管理

设置 → 工具与权限 → 保留的隔离成果会列出原始 `iso_` 成果及 `int_` 整合尝试。
列表只显示观察状态，不会把历史检查通过变成当前工作区已验证。

每条受支持记录声明可执行操作。活动运行、未知格式或待恢复的源整合可能阻止
丢弃预览。无法读取的记录保留诊断，不自动删除。

丢弃需要先预览，再明确确认。预览版本绑定执行身份和完整受支持的候选内容，
包括忽略文件和任务输出；内容发生变化后必须重新预览。

新策略成果持久化 `discard_policy = "host-preview-v1"`。本实现的兼容模式会话
也不能通过模型工具绕过该规则。没有此字段的旧成果保持原有行为。

确认决定先保存内容证明和版本，再清理目录。清理中断后，只能重试删除仍符合
批准内容或仍明确不存在的目录；重新产生的内容需要新的明确决定。

丢弃整合尝试不会删除原始成果。源日志恢复绑定当前工作区、运行时和精确日志
版本；在日志锁内重新检查实际路径授权，持有租约本身不提供权限。

恢复预览同时绑定未完成的 `Applying` 成果记录，因此在文件日志创建前、删除后或
两条成果收据之间中断，也能重新发现恢复入口。

文件恢复与成果状态由同一 Store 协调；尚有活动整合尝试时拒绝等待其生命周期锁。
恢复后的中断成果保留为
`conflict`，验证为 `unknown`，可准备新的整合候选或明确丢弃。日志不存在不会
被解释为已成功发布或已完整回滚，已有源码和候选均不因此被覆盖。

恢复和整合只接受当前写入范围，不使用普通文件工具对工作区外配置文件的预览
豁免。增加协调资源不会扩大授权，也不会在持锁期间请求配置写入审批。

新开的窗格可在明确恢复时建立会话写入权，无需先调用模型。切换工作区后，
未完成的界面读取和预览失效。配对设备不能调用宿主成果管理写入口。

终端界面提供 `/isolated`、`/isolated discard <id>` 和 `/isolated recover`。
预览输出给出携带精确版本的独立 `--confirm` 命令，不会把这些操作发给模型。

### 持久会话执行环境

根会话工作树由独立 `root-environments-v1` 所有者管理，不使用可被成果整合清理的
`entry-v2.json`。准备前记录身份及阶段，快照包含受支持的未提交和未跟踪文件，
并保留 Git 基线引用。关闭运行时只结束执行引用，不删除环境或重置其文件。

会话的 `.environment-v1` 绑定由当前 session 写入授权创建，绑定后不可覆盖。
恢复在扩展和工具启动前核验来源、候选、会话身份及环境代次。相同 cwd 不能代替
有效环境所有权。缺失环境、未知格式、目录逃逸和代次不一致均拒绝执行，保留现场。
热重建先保留独立引用；前台返回和后台任务实际结束前继续持有执行引用。

执行登记与文件租约分开。共享根回合和子代理登记实际工作副本；隔离子代理不占用
源副本的执行位置。普通规划回合可以在同一回合批准执行，故保守登记为可写；只有
宿主强制只读的执行登记为只读。模型思考保留执行登记，不持有文件变更租约。

根成果通过版本化发布记录导出独立 `iso_` 副本，不能应用或删除活动工作树。
声明的依赖在冻结时另存副本，后续导入不再读取持续编辑的根现场。发布游标仅在匹配
成果确认为已整合后推进到导出的根快照；不会将源工作区中别人的新文件纳入根游标。

源已写入但游标未更新时，恢复复用持久终态收据。重试保留旧发布及成果并分配新身份。
Git 路径协议无法重现的非执行位或目录权限变化会明确拒绝发布并保留现场。

根导出、复检及整合已接入回合完成路径；未验证的成果仍保留，不宣称源工作区通过。
管理界面区分执行现场与源工作区，支持重新验证、准备新导出、取消和按预览恢复整合。
CLI/TUI 使用 `/isolated root recheck`、`retry`、`recover`；恢复需确认具体预览。

`agent.auto_integrate` 是用户配置，默认开启；关闭后保留成果，明确的重检整合操作仍可执行。
设置同时显示保存值和当前聊天有效值。检查契约在现场变为可写前由宿主冻结，重启不会
用现场修改后的检查规则替换它。旧程序可能忽略环境绑定；安全降级写入仍须验收。

### 数据兼容

完整格式、旧读写器行为和保留边界见[协议兼容矩阵](CONCURRENT_MUTATIONS_PROTOCOL.md)。

### 验收与保障边界

短事务、根任务自动选择、task/fleet 生命周期与共享预算、恢复界面、受控整合及
配置迁移已完成分批源码、故障、兼容、三平台与性能验收。新安装启用 `auto`，
已有配置保持原选择，活动运行不在中途切换。验收记录保留失败首轮及复验范围。

macOS/Windows 可使用显式的本地 Linux 容器验证；这不改变候选任务自身 Bash/hook
的写入边界。未获得可靠验证的嵌套成果保留在各自候选中，
不能静默应用到最外层源。无宿主环境工厂的旧运行时仍拒绝显式嵌套 worktree。
`Outcome.Unverified` 为空不构成正向凭据，不能据此自动应用结果。

worktree 隔离代码副本，不隔离数据库、端口、网络服务或全部 Git 共享元数据。
文件协调只约束遵守协议的写入者；外部写入仍存在检查与替换之间的竞争窗口。
macOS 测试不替代 Windows 和 Linux 的实际运行记录。

### 候选进程的文件变更边界

task、持久根环境、旧隔离任务及 best-of 候选都要求完整的创建、覆盖、删除与
重命名边界。候选只开放自身写根和独立临时目录，不继承宿主的广泛缓存写权限。
默认 Go、npm、pip、uv 缓存放在候选的私有临时目录；用户显式设置的环境变量保留，
但不能据此扩大写权限。启动时再次核对源保护路径，临时目录与源重叠也会拒绝。

Windows 现有创建限制令牌无法提供这个完整边界，因此候选 Bash 和程序 hook
在启动前明确拒绝；会话级逃逸批准不能绕过。候选的进程内文件工具继续按授权运行。
程序 hook 的拒绝通过宿主诊断显示，不重放候选的模型流或工具轨迹。

隔离环境的 Go 基线检查使用声明的离线根文件系统或本地容器，在冻结副本中恢复
宿主持有的测试正文，经原工具、权限、扩展和 hook 管线执行。未配置受控运行环境、
工具被禁用、权限拒绝或后端不支持时保留 unavailable，不直接在宿主启动候选代码。

捕获测试凭据绑定正文与执行身份，不能代替普通整合凭据。判定独立消费完整输出流，
只认父测试终态；展示裁剪、子测试通过及取消均不能制造通过。

正向结算还要求整个命令与宿主收尾成功；收尾 hook 改动源码或后续输入变化会使凭据失效。

共享环境保留旧 overlay 基线语义；取消、启动失败或完成未知不会用已有局部 pass
结算该义务。普通进程 wait 不是任意后代都已结束的证明，不能代替受控验证器的执行域凭据。

## English

This document describes the implemented protocol and its local qualification
boundaries. See the [acceptance record](CONCURRENT_MUTATIONS_ACCEPTANCE.md).

New installations default to `auto`. Existing configurations without a policy
retain legacy behavior and old switches. The three strategies cover initial
execution and persistent restoration through CLI, serve and ACP. Hosted CI,
upstream merging and publication remain separate delivery actions.

See the [fixed-load coordination evidence](CONCURRENT_MUTATIONS_PERFORMANCE.md)
for paired results and P95 regressions. It does not qualify product performance.

### Authorization and transactions

Controller staging, commits, discard and branch switches use coordination too.
The Git snapshot lease ends before commit-message generation. User shell
commands retain physical ownership until process completion, then release it
before model follow-up. Queued discard rechecks file, index and HEAD versions.

Opaque Bash and hooks include known primary/extra-root GitDir/CommonDir scopes.
A permission classification as read-only does not replace the tool's mutation
contract: Git queries can still update caches. This grants no new write access
and does not identify every external repository an arbitrary script may name.

After admission, shell operations rediscover GitDir, CommonDir and WorkTree.
Changed `.git` or `commondir` pointers refuse execution. Host Git commands keep
their pinned environment.

Push preparation materializes independent objects;
source transactions end before network transfer and resume only for tracking-ref
CAS. A local settlement conflict preserves remote success and does not prompt a
repeat push.

`CapabilityGrant` owns authorization. Assignment scope does not imply exclusive
ownership. New transactions cover mutation, required hooks and mutation
observation; model inference does not retain a previous write's execution lease.

`MutationImpact` belongs to the concrete tool contract, independently of
authorization and evidence classification. Shell checks may generate files and
conservatively coordinate the workspace. Extra metadata resources grant no
additional write authority.

Resolve the concrete tool, authorize the call and any path expansion, then
request its complete resource set. Conflicting requests receive FIFO service;
disjoint requests can proceed. A 30-second file-resource wait limit returns a reschedulable
failure and never steals resources.

The cross-process registry lock protects registration, not tool execution.
Git discovery and legacy claim publication run outside that critical section.
A separate preparation OS lock retains queue priority while compatibility
publication runs, distinguishing a transaction's own claim from a legacy blocker.

Per-transaction OS liveness locks distinguish dead owners. Unique transaction IDs
prevent an old callback from releasing a new lease.

Unknown or malformed records are preserved and refuse coordination.

Each parent and child agent has an independent read-version record. Whole-file
replacement requires a matching version previously read by that agent.

Versions come from the same read stream, never a second open. Reads exceeding the
tracking bound do not establish an overwrite version.

Approval previews are recomputed under the lease and checked again after hooks.
A changed preview stops application and requires a fresh read and preview.

Active editor buffers require version-checked writes. An unsupported buffer
refuses rather than silently falling back to disk. Unsaved buffers still require
read authorization; a missing disk file does not grant access to buffer contents.

Subagent start/stop hooks each use a short transaction, excluding model work.
Unconfirmed process cleanup retains physical ownership after cancellation;
the ended caller immediately loses execution authority.

Wait events carry transaction, execution, session, call and holder identities.
Closing events retain the transaction ID and distinguish cancellation, timeout,
version conflict and admission refusal.

Desktop and TUI match and replay waits
idempotently without closing a sibling's wait. Identity-free legacy events match
only legacy waits.

### Integration and recovery

Finished candidates record content hashes from actual bytes, types and modes,
including ignored files. Limits, missing inputs, external links and nested
repositories make the observation incomplete and suppress its digest.

The user setting `agent.isolation_copy_paths = ["deps"]` declares independent
copies of repository-relative inputs. Project configuration cannot expand it.
Nothing ignored is copied by default. Regular files and empty directories retain
modes.

Links, special files, sensitive names, Git metadata and denied paths refuse
preparation.

Preparation jointly coordinates source, candidate and Git metadata and checks
actual source and destination bytes. Copies share no writable directories or
hardlinks with source. The durable preparation receipt records declaration and
content digests, not external-environment completeness or verification success.

Git content snapshots also carry versioned ordinary permission bits. Candidate
materialization restores captured file, parent-directory and root modes, independent
of checkout umask or later source chmod. Gitlinks retain directories without copying
submodule contents.

Typed tree entries require complete metadata coverage; both
encoding and decoding enforce an 8 MiB limit, with object size checked before reading.

An additional candidate HEAD parent anchors the metadata against ordinary Git GC.
The first parent remains the source HEAD; source refs and index are unchanged.
ACLs, extended attributes and special permission bits are outside this record.

The initial baseline covers the entire supported candidate domain. Ignored
outputs outside declared paths also prevent automatic cleanup. Apply and
integration refuse changes outside the Git manifest and retain results for
reconciliation or explicit discard.

Old results remain readable; absent complete baselines, unknown extra contents
prevent automatic application and cleanup.

`workspace-content-v1` excludes top-level Git metadata and external environment.
It binds finished content only: verification stays unknown until checks, their
inputs, Git state, environment and commit-time freshness are separately bound.

After model execution, the host reruns frozen `verify:` commands against the
candidate through a fresh executor and the ordinary tool pipeline. Original
tool/path grants, permissions, hooks, sandboxing and transactions still apply.
Disabled Bash stays disabled, and replaced check commands are refused.

Each check owns its execution identity and evidence. Success requires a trusted
`passed` verdict as well as exit zero. Cancellation and exceptions still seal
the candidate and release run ownership. Ordinary observations remain unknown as
workspace verification; owned controlled-environment receipts are required.

On Linux, user-owned `agent.verification_runtime` declares an offline root
filesystem. The host freezes source and runtime files, mounts them read-only,
isolates PID/network namespaces and temporary directories, and clears inherited
environment variables.

A startup handshake fixes PID1 identity; cancellation
and cleanup await actual scope termination. Unproven termination retains resources.

Receipts belong to the current process; persisted flags cannot forge them and
restart requires a recheck.

All hosts can also explicitly select a local immutable Linux image through
[container verification](CONTROLLED_CONTAINER_VERIFICATION.md), using a Unix
socket or Windows named pipe. Ordinary observations remain unknown.

The final launch boundary records hashes of actual argv, environment and command,
interpreter content, cwd/platform, content before/after and physical wait
completion. Raw commands and environment values are not persisted. A resumed
generation clears current checks, including when runtime preparation fails.

Successful host checks prepare a separate `int_` integration attempt from the
current source plus the original delta, then rerun checks there. `.integration-v1`
retains its lifecycle without rewriting the original `iso_` baseline or manifest.

Checks retain no source-file lease. Preparation uses the root pool and releases
its permit on exceptions.

Publication rechecks result generation, source content/Git inputs, candidate
content and the original source conversation's live write authority. Journal
recovery authorizes its actual paths under the journal lock. A whole-tree
coordination scope is not write permission.

The complete projected postimage
must match the checked candidate before any source write. Journals retain result
and attempt IDs; retries preserve terminal receipts.

Production controlled receipts can authorize automatic checking and
integration after source freshness and authorization are revalidated.

Ordinary launch observations retain Git/external-environment gaps: those attempts
remain `unknown` and retain both candidates.

Test coverage substitutes exercise only
the commit state machine and do not replace real controlled-runtime acceptance.
Explicitly discarding an attempt preserves its original result.

`Apply` and `ApplyPaths` use one integration implementation. `Apply` additionally
requires the whole source checkout to match its snapshot; `ApplyPaths` compares
each affected path against its baseline and target, including mode and file type.

The per-checkout Git directory holds `reasonix-apply-v1/active.json`. The user's
index is unchanged. A durable journal records raw preimages and target images
before individual atomic replacements. This is not an atomic multi-file commit:
observers may see intermediate states.

Recovery restores only files still matching the transaction's postimage.
Files unchanged from their preimage need no restoration. External edits retain
both the file and journal and require recovery.

Retrying integration first settles the journal and then rechecks the baseline.
Every strategy in the current program refuses ordinary writes while a journal
is unfinished, including a live owner's journal. The coordinator's queue uses
its separate liveness check. Switching to legacy cannot bypass recovery.

Tools, observation and coordination derive their domains from the same final
write roots, including `sandbox.allow_write` and an overridden workspace root.
`WorkspaceOnly` retains only the isolated scene. Rebuilds that reuse a workspace
owner retain old runtime barriers and add newly authorized repository domains.

Recovery tools must claim all journal paths, including paths outside the new
candidate's change list.

The host checks the live lease scope again under the journal lock. Missing
recovery authorization refuses rollback. New strategies expose
`recover_integration`, which does not require another isolated candidate.

An isolated result persists preparing state, identity and generation before Git
creates its worktree. Preparing, running, pending, applying, integrated,
interrupted and recovery-required states are distinct. Interrupted preparation
retains its record and partial environment; restart never implicitly resets it.

Explicit discard removes registered candidates. Unregistered remnants whose
ownership cannot be verified remain intact.

Failed, cancelled and conflicting results retain distinct terminal states and
can be explicitly continued or settled. Deadlines count as cancellation, matching
the delegated transcript's terminal state.

Runner, integration and cleanup failures do not report success. Modified,
unintegrated results do not expire after 14 days. Only idle candidates proven
unchanged are eligible for automatic expiry.

Apply and discard share a cross-process per-result lock.

Operations read the manifest, acquire all resources, then lock the result and
recheck generation and content. No result lock is held while waiting for file
or Git resources. Sealing a candidate also waits for outstanding writer cleanup.

New results use `entry-v2.json`; legacy `entry.json` sweepers cannot discover
them. Active legacy markers remain at their original location. Only terminal
legacy records migrate: copy the complete receipt, verify matching raw fields,
remove the legacy marker, then clean the candidate. Unknown fields survive.

An older process that already loaded a legacy record can still run its old
cleanup logic; this remains a cross-version boundary.

After cleanup, `entry-v2.json` retains the result ID, baseline, manifest, evidence
and disposition. Cleanup completion is recorded separately. Death after candidate
deletion can be retried without reapplying source files. The opposite disposition
refuses.

Explicit discard of a drifted candidate preserves its sealed manifest.

Pending results are read from disk, without a stale controller-local cache.
Allocation markers prevent result ID reuse. Corrupt identity records remain and
refuse new allocation until explicitly recovered.

### Compatibility

Under a new strategy, task isolation accepts `auto`, `shared` and `worktree`.
Explicit worktree tasks share profile, tool/path grant, model/effort, step-limit,
scheduling and background execution code with shared tasks.

New-mode children use independent root-owned pools for model requests,
mutations and preparation, instead of holding one slot for their lifetime.

Mutation capacity and file resources are acquired together. A file-blocked
request consumes no capacity, and smaller requests cannot starve an older
larger request in the same pool. Disjoint pools can proceed.

Model capacity is held from before dispatch until the provider source channel
closes. Cancellation ends the consumer wait before physical release; retry
backoff holds no permit. Auxiliary model calls use the same entry without
wrapping provider traits or changing request bytes.

A remote broker's local channel closure does not prove upstream completion;
broker-host admission remains unfinished. The root budget here governs
concurrency resources, not aggregate monetary accounting.

Git snapshots and worktree creation acquire preparation capacity together with
their paths. Kernel construction and child registry/transcript preparation use
separate short preparation phases. Cancellation in the initial preparation
queue creates no candidate.

Failures after a preparing record exists retain
recovery information. Model/tool waits retain no preparation permit.

Each root admits at most `max(64, max_subagent_concurrency)` resident children,
including model waiters. Excess requests return `execution.resident_limit`
before creating a candidate or background job.

Collect or cancel existing
work before retrying. Nested children never queue behind their own resident
parent. This bound is separate from model concurrency and does not cap retained
completed results.

Same-session runtime replacements and isolated kernels retain the root pool
identity and effective limits. Live writer counts remain independent of phase
permits.

Existing `max_subagent_concurrency` and `max_parallel_writers` limits
still apply to children; the root's primary request uses its own lane. Settings
and Desktop distinguish saved strategy from the current runtime's actual strategy
and child/writer limits.

Effective values come from assembled owners, including
retained limits after rebuild. Saving never switches active operations.

Explicit-Home edits target that Home's user configuration, use strict reads and
cross-process serialization, and save with user scope. Legacy optimistic and wait
choices remain available in compatibility settings.

First execution selects isolation from cross-process presence for the actual
working copy. An already-started conversation never migrates automatically.

Unsupported composition rebinding refuses isolation. Automatic fallback to
shared execution retains the same persona, tool ceiling and transcript binding.
Background contexts carry declared identities and preferences, not source
approvers or background starters; child executions use their own host services.

New-mode read-only skills use the same execution entry while retaining ephemeral
transcripts and their existing review-authority boundary. Completion protocol
tools are fixed at runtime construction; identical invocations keep stable
system and tool prefixes.

Background shell jobs:

- acquire their own transaction before launch and release it once the process
  has started; queue cancellation does not launch the command;
- run as external writers afterwards, holding no workspace domain, so a
  long-lived service never blocks other writers;
- remain visible to named file writes through their version checks.

Explicit shell detachment under the new protocol is refused. Run services
directly with `run_in_background` to retain status and cancellation. The protocol
does not promise to contain arbitrary self-detaching external programs.

Transcripts record their environment and origin workspace. The owning chat can
continue a retained candidate with `continue_from`. Resume, apply and discard
exclude each other.

Unchanged resumable environments are retained, but remain eligible for idle
expiry. Root conversation forks reopen independent scenes. Ancestor task
continuation creates and reuses a conversation-owned copy.

Interrupted runs retain files and require a new generation; preparation that
never produced a runnable scene remains recovery-required. See the
[continuation guide](CONCURRENT_MUTATIONS_CONTINUATION.md).

Each execution persists a new generation. Old callbacks cannot finish or delete
a successor. Interrupted transcript recovery requires a new generation in the
same environment, with persona, tools and model identity checks retained.

New runtimes with a host environment factory support nested worktrees targeting
their direct parent. Shared and read-only choices retain their existing behavior.
See the [nested result guide](CONCURRENT_MUTATIONS_NESTED.md) for registration,
authorization, retention and recovery.

Each isolated writer execution owns a separate background-job manager and
execution identity. It never borrows a parent's or sibling's job table. When
permitted by the tool whitelist, it can use background Bash, `wait`,
`bash_output` and `kill_shell`; read-only children retain no background capability.

Before returning, the parent atomically closes job admission and joins registered
jobs and tracked OS waits before releasing temporary directories, verifying or
sealing its candidate.

Cancellation stops owned jobs but cannot substitute for
physical completion; an unconfirmed OS wait retains the environment. This does
not establish containment of arbitrary detached external processes.

After background jobs join, their evidence enters the actual executing agent's
ledger before the completion handoff is generated. Unordered concurrent checks
cannot verify those mutations; rerun them after collecting all changes.

Collection imports mutation observations only. Commands, reads and reviews
without a shared snapshot identity remain in the original job record and cannot
prove the collector's current version. `wait`, `bash_output`, turn-start
recollection and execution teardown use the same rule.

Completion reports reject checks predating the latest mutation. The handoff
shows each command's latest outcome and marks passing checks invalidated by
later changes.

Foreground and background isolated execution use a candidate-local evidence
ledger. Candidate checks never become source-workspace checks through child or
job evidence merging.

Durable observations retain tool/execution identity,
paths, exit status and output digests, excluding raw arguments and command text.
Their verification status remains `unknown` and cannot authorize integration.

### Retained result management

Settings → Tools & Permissions → Retained isolated results lists original
`iso_` results and `int_` integration attempts. The list is an observation;
recorded success never becomes current workspace verification merely by listing.

Each supported record advertises its available actions. Active ownership,
unknown versions and source integration recovery can prevent a discard preview.
Diagnostics retain unreadable records rather than deleting them.

Discard requires a preview and a separate explicit confirmation. Its revision
binds the execution identity and complete supported candidate contents, including
ignored files and task outputs. Changes require another preview.

Coordinated results persist `discard_policy = "host-preview-v1"`. A legacy
runtime of this implementation cannot use the model tool to bypass that rule.
Existing records without this policy retain legacy behavior.

Accepted discard decisions persist their content proof and revision before
cleanup. Interrupted cleanup only retries against the approved content or a
still-absent directory. Recreated content is retained until another decision.

Discarding an integration attempt preserves its original result. Source journal
recovery binds the current workspace, runtime and exact journal revision. Actual
paths are reauthorized under the journal lock; a lease supplies no permission.

Recovery previews also bind unfinished `Applying` records, covering crashes
before journal creation, after journal removal and between result receipts.

One Store coordinates file recovery and lifecycle reconciliation. Active attempts
refuse recovery without waiting on their lifecycle locks. Interrupted results
remain as `conflict` with `unknown` verification, ready for a new integration
candidate or explicit discard.

A missing journal establishes neither publication
nor rollback, and never causes retained source or candidate files to be replaced.

Recovery and integration require current write scope. They cannot borrow the
ordinary file tools' preview exception for out-of-workspace configuration files.
Coordination resources grant no permission, and no configuration approval is
requested while holding resources.

Fresh panes establish session writer authority when explicitly recovering,
without requiring a model turn. Switching workspaces invalidates pending UI
reads and confirmations. Paired devices cannot invoke host management writes.

The terminal UI offers `/isolated`, `/isolated discard <id>` and
`/isolated recover`. Preview output supplies a separate `--confirm` command
carrying the exact revision. These commands never become model input.

### Persistent session environments

Root session worktrees have a separate `root-environments-v1` owner. They never
use disposable result markers. Preparation records identity and phase before
changing Git state; baseline refs retain supported dirty snapshots. Closing a
runtime releases execution references without deleting or resetting its files.

The transcript's immutable `.environment-v1` binding requires current session
write authority. Restoration validates source, candidate, session and generation
before extensions or tools start. Matching cwd alone grants no ownership.

Missing environments, unknown formats, path escapes and stale generations refuse
execution and retain the records. Rebuilds acquire their own references first;
foreground turns and background jobs retain ownership until actual completion.

Execution presence is independent of file leases. Root turns and delegated work
register the actual working copy; isolated children do not occupy source copies.

Planning can approve execution within one admitted turn, so it conservatively
registers as writer capable. Only host-enforced read-only executions register as
readers. Model reasoning retains presence without retaining file mutation leases.

Root publication freezes an independent `iso_` result, never a disposable live
worktree. Declared dependencies are copied at freeze time; import never rereads
the continuing root scene. A matching integrated receipt advances the cursor
only to the exported root snapshot, excluding unrelated new source files.

Restart can acknowledge an already-integrated result. Explicit retry retains
old records/results and allocates a fresh identity. Unsupported non-executable
or directory permission changes refuse publication and retain the scene.

Turn completion now invokes export, recheck and integration. Unverified results
remain retained without source verification claims. The manager distinguishes
live/source paths and supports recheck, fresh-export preparation, cancellation
and exact-preview source recovery.

CLI/TUI uses `/isolated root recheck`, `retry`
and `recover`; recovery requires confirmation of the preview.

User-only `agent.auto_integrate` defaults on. Disabling it retains results;
explicit recheck-and-integrate remains available. Settings show saved and runtime
values separately. Host checks are frozen before the scene becomes writable;
restart cannot replace them with changed scene instructions.

### Protocol compatibility

See the [protocol compatibility matrix](CONCURRENT_MUTATIONS_PROTOCOL.md) for
format versions, previous-reader behavior and retention boundaries. Bound
sessions publish a schema 6 envelope as the first WAL record; the qualified
previous reader and writer refuse it before checkpoint fallback.

Maintenance
preserves it and interrupted sidecar publication is recoverable. Unaware old
programs in independent sessions can still ignore source recovery journals;
that downgrade is outside safe compatibility.

Short transactions, root selection, task/fleet lifecycles and shared budgets,
recovery UI, controlled integration and migration have fixed-source functional,
fault, compatibility, native-platform and performance qualification.

New installations use `auto`; existing choices and active executions retain
their policy. Qualification records preserve failed first attempts and exact
replacement scopes.

macOS/Windows can use explicitly provisioned local Linux container verification.
This does not change the candidate task's own Bash/hook mutation boundary.

Unverified nested results remain
in their own candidates without being applied to the outermost source. Legacy
runtimes without a host environment factory still refuse explicit nested worktrees.

An empty `Outcome.Unverified` is not positive verification evidence and must not
authorize automatic integration.

Worktrees isolate code copies, not databases, ports, network services or all
shared Git metadata. The protocol coordinates participating writers. External
writers can still race the final check and replacement. Local macOS tests do
not substitute for actual Windows and Linux evidence.

Candidate processes require complete file mutation bounds for task, persistent
root, legacy isolation and best-of execution. Only candidate write roots and
private temporary storage remain writable.

Default Go, npm, pip and uv caches use
private temporary storage; explicit user environment choices remain unchanged
without expanding write authority.

Final launch preparation checks the protected
source roots again, including the actual temporary directory.

The existing Windows creation-only token cannot provide this boundary. Candidate
Bash and program hooks refuse before launch, and session escape approval cannot
override the requirement. In-process file tools retain their granted access.

Host hook diagnostics reach the source without replaying the candidate's model
stream or tool transcript.

Isolated Go baseline checks restore host-held test bytes only in a frozen copy
of the declared offline root or local container runtime and source. They use the
original tool, permission, extension and hook pipeline. Missing runtimes,
disabled tools, denied access and unsupported backends remain unavailable.

Candidate code never falls back to direct host execution. Captured-test receipts
bind the bytes and execution identity and cannot authorize ordinary integration.

An independent bounded streaming observer consumes the complete output and
requires each parent test's terminal event. Display truncation, passing subtests
and cancellation cannot establish a pass.

Positive settlement also requires the
whole command and host finalization to succeed. Post-hook source changes or
later input changes invalidate the captured verdict.

Shared execution
retains legacy baseline semantics; cancellation, launch failure and unknown
completion cannot settle an obligation from partial passing output.

Ordinary
process wait does not prove every descendant has ended and cannot replace the
controlled verifier's execution-scope receipt.
