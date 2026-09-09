# 文件读取、证据检查与进度展示改造（单 PR 方案）

状态：实施中。分支 `feature/read-evidence-pr1`，基线 `main-v2 / daf18272f`。
一个 PR 承载全部改造；内部按可审查提交推进，不再拆分为多个 PR。

## 1. 目标与默认行为

普通用户无需配置开关、选择技术模式或学习新操作，即可获得以下默认行为：

1. 普通局部查阅不产生全文读取债务，不要求模型提交机械回执。
2. 明确全文任务可正确分页；无法完成时说明缺口，不虚报成功。
3. 文件读取受阻只影响依赖它的操作，独立工作仍可继续。
4. 修改前检查实际目标范围及内容是否仍有效。
5. 重复读取有界退出，正常分页只更新一条进度状态。

不包含：增大默认上下文、放宽修改权限、移除截断标识、重做会话日志、通过读取完成标记证明模型已正确理解代码。

## 2. 基础契约

三个概念必须分开，不能互相替代：

- **来源身份**：工作区、规范路径、实际数据来源（磁盘文件或编辑器 overlay），以及实际生成输出的原始字节或缓冲区的 SHA-256。不能在执行完成后重新探测来源、再给旧输出补发身份。
- **读取快照身份**：同一次逻辑读取所使用的稳定内容版本，跨页保持不变，源内容变化时改变。局部读取不为此强制扫描全文。
- **窗口摘要**：该页实际交付内容的摘要，只证明这一页，不能替代整文件版本。

协议为 v2；v1 字段仅用于历史诊断，不授予修改许可。

- 第一次读取创建逻辑 `read_id`；后续分页沿用它，每次工具调用仍保留独立调用 ID 与 `result_ref`。
- 游标由主机签发，绑定会话、运行代次、读取任务、规范路径、来源快照、请求范围和准确下一位置；校验发生在执行入口，能解码不等于被接受。
- 拒绝跨会话、跨文件、过期、伪造及不匹配位置的引用；恢复运行时旧游标失效。
- 模型可以使用续读引用，无需理解其字段。
- `EOF` 必须携带可信的源结束位置；不能仅因某页出现 EOF 就判定整个 range 完成；空文件必须有明确的空文件事实。
- 依据最终模型可见输出裁剪证据：被截断的半行、未交付原文、摘要和搜索结果不计作完整原文覆盖。
- 同一 provider batch 的新读取不能为该批次的修改提供证据。
- 协调器返回深拷贝快照；取消、版本变化及新一代任务使用 generation fencing。
- 上下文压缩后的历史证据保留审计价值，修改所需的当前可见片段仍须重新获取。

## 3. 读取语义与统一协调器

| 调用 | 默认行为 |
| --- | --- |
| 无范围、无 intent | `inspect`：有界预览，文件有剩余内容不产生全文债务 |
| 有 offset 或 limit、无 intent | `range`：只要求指定范围，按可信源结束位置截止 |
| `intent=full` | 建立不可由模型自行降级的全文义务，用续读游标推进 |
| 冲突参数 | 返回明确参数错误和合法调用方式 |

全文义务来源包括明确用户要求、适用项目规则和已确认任务要求；任务契约保留其来源，不能因后续工具改用 `inspect` 而解除。自然语言任务理解仍属模型能力，不宣称主机能形式化识别所有全文审查意图。

协调器接入真正的默认执行路径，统一负责意图、预算、进展、完成与暂停；旧状态机不再同时 enforce。

## 4. 按操作检查证据

主机内部 `CheckOperationEvidence`：真实 writer 声明目标和所需证据，复用其路径、编码、overlay、唯一匹配及区间解析，不信任模型自报的写范围。

| 操作 | 处理规则 |
| --- | --- |
| 搜索、列目录及可信只读工具 | 保持可执行，不因其他文件读取未完成而阻塞 |
| 创建新文件 | 不要求读取不存在的内容；执行时仍检查目标不存在条件及授权 |
| 精确编辑 | 要求目标旧文本及必要上下文已交付，执行前重新验证 |
| 范围或符号删除 | 要求完整目标、两端锚点及中间内容；替换旧的“必须无参数重读全文”fallback |
| 覆盖已有文件 | 默认要求完整当前内容证据；只有明确授权完全重建时例外 |
| notebook、移动等其他内置写工具 | 按真实影响声明证据，不静默视为安全 |
| shell/MCP 等未知写范围 | 保持原有授权边界；存在可能相关的未满足修改证据时保守阻止 |
| 最终回答 | 允许报告局部结果或明确阻塞；未满足的明确全文任务不得标记为整体完成 |

`multi_edit` 仍是单文件原子编辑，本 PR 不改造为多文件事务工具。多目标操作与已声明写范围的工具批次执行统一证据预检；实际写入继续遵守原有原子性语义。

## 5. 有界续读与成本控制

`needs_scope` 接入真实上下文预算：复用现有有效上下文、压缩阈值及任务预算，不扩大默认上下文，不自动压缩以强行塞入全文。

- 初始内部上限：每个逻辑读取最多自动推进 64 页、累计主动读取执行时间 120 秒；同时受动态 token 预算与任务总预算约束，最先达到者生效。等待模型响应不计入主动读取执行时间。
- 初始 inspect 页沿用现有行数与输出上限；正常局部读取不自动扫完文件。
- 连续两次无新增有效内容：切换一次有依据的策略；切换后再连续两次无进展则暂停该依赖链。
- 重复页、重复搜索、换调用 ID 不算进展；文件变化不重置硬预算。
- 显式全文任务预算不足时保留未完成范围，继续独立事项，不自行降级。
- 旧策略回执保留为兼容适配器，依据主机证据验证；新流程不要求调用。
- 由现有通用进度保护统一决定暂停，避免一次行为被多个计数器重复处罚。

这些上限是内部默认值，不增加用户设置项；调整必须依据资源数据并记录。

## 6. 状态展示

带版本的读取状态事件，以会话、回合、`read_id`、generation 和 sequence 定位：

- 前端按读取任务 upsert 一条状态；正常分页使用中性进度，不逐页追加警告。
- 局部页完成只在工具卡展示返回范围及是否还有内容。
- 全文任务显示已覆盖范围；总量未知时不显示虚假百分比。
- 暂停时显示原因、缺失范围及可执行恢复动作，默认折叠诊断细节。
- 活动状态由稳定宿主管理，避免分页更新改变历史消息几何。
- 覆盖取消、并行读取、会话切换、断线重连、乱序事件和历史恢复。
- CLI、serve、ACP 使用同一语义；旧客户端只收到有界起止及阻塞 Notice。
- 完成 en、zh、zh-TW 文案与无障碍通知节流。

## 7. 验收

协议与证据：多页共用稳定读取身份并正确累计覆盖；CRLF/LF、BOM、UTF-16、同大小替换、overlay 修改、符号链接变化均不能错误复用证据；半行截断、搜索摘要、旧游标、跨会话引用及单独 EOF 不能伪造完成；同批 read/edit、取消后结果、压缩后修改及恢复后的旧元数据按契约处理。

实际任务：大文件局部查询不强制全文、不要求回执、不阻塞独立操作；全文审查多页交付后完成、不足时准确暂停；A 文件受阻不阻塞 B 文件且整体任务不虚假完成；覆盖、范围删除、精确编辑和多目标预检分别有通过、缺失、过期及并发变化用例；重复页与重复 grep 按“两次换策略、再两次暂停”结束，微小进展不能绕过总预算。

界面与质量：连续 100 次进度更新只更新一条活动状态、无重复警告与列表跳动；Linux/macOS/Windows 路径与编码行为，以及 Windows WebView2、macOS WKWebView 真实界面；固定任务集比较成功率、介入次数、工具轮数、输入 token、耗时与压缩次数；host-only 元数据在 Chat、Responses、Anthropic 实际序列化中均被剥离；schema 保持确定顺序，动态状态不进入稳定前缀；最终运行 race、lint/vet、根模块与 desktop 测试、前端完整测试计划、生产构建与资源预算。

禁止交付：错误放行、虚假全文完成、无限续读、跨任务证据串用。

## 8. 实施顺序与状态

| 步骤 | 内容 | 状态 |
| --- | --- | --- |
| 1 | 来源身份、稳定读取 ID、游标及覆盖判定 | 已完成（19d91b570） |
| 2 | 真实 writer 证据要求与统一预检 | 已完成：writer 声明、统一预检、批次级预检先于执行、未知写范围保守阻止、用户显式重建授权 |
| 3 | 预算、有界续读及旧回执兼容 | 已完成：预算与阶梯、旧策略回执作为兼容适配器保留（仍按主机证据校验，5 个测试覆盖），新流程不要求调用 |
| 4 | 结构化事件、单卡 UI、跨端与本地化 | 已完成：事件、桌面状态行、CLI 状态行与 en/zh/zh-TW 文案 |
| 5 | 默认启用新协调器，移除冲突的旧执行路径 | 已完成：新行为即默认，Options.ReadPipeline 只保留主机内部回退开关 |
| 6 | 方案文档、工具说明、兼容说明与验收记录 | 已完成：工具说明、方案、验收矩阵与固定任务集对比 |

默认行为已切换：无范围、无 intent 的读取是有界预览，不再产生全文债务；只有 `intent=full` 会分页到结尾。跨平台界面验收（Windows WebView2、macOS WKWebView）与固定任务集对比需要在具备真实桌面的环境执行，不能由本仓库的单元测试替代。

默认使用新行为；仅保留主机内部、按 turn 固定的回退入口，用于诊断和紧急回退，不暴露普通用户开关。旧活跃状态不能在半个工具批次中转换；恢复时重新验证，回退不撤销用户文件中已完成的修改。

## 9. 验收记录

以下每项都由仓库内的确定性测试覆盖；命令为 `go test ./...`、`pnpm test`（桌面前端）与 `make lint`。

协议与证据：

| 验收点 | 覆盖测试 |
| --- | --- |
| 多页共用稳定读取身份并累计覆盖 | `TestRangeObligationPagesUntilCovered`、`TestOutOfOrderPagesStillSatisfyAWholeFileRead`、`TestReadContinuationCursorJoinsTheLogicalRead` |
| 不同窗口摘要不被误判为文件变化 | `TestReadEnvelopeNamesTheServingStore`、`TestReadEnvelopeSeparatesSourceIdentityFromWindowDigest` |
| CRLF/UTF-16/编码差异不错误复用证据 | `TestReadEnvelopeKeepsUnicodeWindowsIntact`、`TestReadEnvelopeSeparatesSourceIdentityFromWindowDigest` |
| 半行截断不计作已读 | `TestClipToNarrowsDeliveredRangeToVisibleBytes`、`TestReadShadowRecordsTheCoordinatorVerdict` |
| 单独 EOF 不能伪造完成 | `TestRangeCompletionNeedsATrustworthySourceEnd`、`TestWholeFileRequiresContiguousCoverageFromLineZero` |
| 旧游标、跨会话、跨文件、过期引用被拒绝 | `TestReadContinuationCursorRejections`、`TestReadCursorRoundTripAndMatching` |
| 同批 read 不能为同批修改作证 | `TestEvidenceGateIgnoresSameBatchReads` |
| 跨快照分页不拼接 | `TestEvidenceGateNeverStitchesAcrossSnapshots`、`TestEvidenceGateStitchesPagesOfOneSnapshot` |
| host-only 元数据被剥离 | `TestReadResultEnvelopeDoesNotAffectProviderVisibleBytes`、`TestModelInputMessagesStripsReadResult` |
| 协调器状态不可被外部修改 | `TestReturnedObligationsAreDeepCopies` |

实际任务：

| 验收点 | 覆盖测试 |
| --- | --- |
| 大文件局部查询不强制全文 | `TestImplicitReadIsABoundedPreview` |
| 明确全文任务分页到 EOF | `TestExplicitFullReadContinuesSourcePagesToEOF` |
| 覆盖/删除/精确编辑的证据规则 | `TestEvidenceGateBlocksAnUnreadOverwrite`、`TestEvidenceGateAllowsAfterTheModelSawTheContent`、`TestEvidenceGateRejectsStaleContent`、`TestWriteFileDeclaresWholeFileEvidenceOnlyForOverwrites` |
| 未知写范围不绕过证据阻塞 | `TestEvidenceGateBlocksUnknownScopeWriterAfterABlock`、`TestEvidenceGateLeavesUndeclaredWritersAlone` |
| 批次级预检先于执行 | `TestEvidencePreflightBlocksBeforeTheBatchRuns` |
| 用户显式重建授权（模型不能自授） | `TestEvidenceGateHonorsAnExplicitRebuildInstruction`、`TestParseConstraintsRecognizesAnExplicitRebuild` |
| 重复页与无进展有界退出 | `TestRepeatedPageIsNotProgress`、`TestStalledPagesPivotOnceThenPause` |
| 预算耗尽与内容变化不重置 | `TestPageBudgetStopsContinuation`、`TestActiveTimeBudgetStopsContinuation`、`TestContentChangeDoesNotResetTheBudget` |
| 未知上下文窗口不猜测 | `TestReadShadowNarrowsAnUnboundedFullRead` |
| 固定任务集的新旧策略对比 | `TestFixedTaskSetComparesReadPolicies`（同一脚本任务在新默认与旧回退开关下运行，记录轮数、读取次数、主机续读指令数与放行写入数） |

界面：

| 验收点 | 覆盖测试 |
| --- | --- |
| 连续 100 次更新仍只有一条活动状态 | `read-status-upsert.test.ts` |
| 乱序事件不回退 | `read-status-upsert.test.ts` |
| 新回合清空上一回合状态 | `read-status-upsert.test.ts` |

跨平台：`go test` 在 CI 的 ubuntu-latest、macos-latest、windows-latest 三平台矩阵上运行，因此路径、换行与文件替换的确定性用例由 CI 覆盖；本机只验证了 macOS。

仍需真实环境执行：Windows 原生 WebView2 与 macOS WKWebView 的界面与滚动验收（需要真实桌面与交互）；真实 provider 下的任务集成功率与 token 对比（本仓库提供确定性 harness 覆盖轮数、读取次数、主机续读指令与放行写入数）。ACP 消费同一结构化事件，终端侧由 CLI 渲染。

## 复核修复与验收补充

以上提交映射是历史记录，不代表仅凭协调器单测即可完成验收。执行链路补测以 `read_pipeline_regression_test.go` 为准：新协调器实际控制续读、暂停和最终回答；旧状态机仅在内部回退模式运行。普通 inspect 继续正常参与上下文压缩，动态预算裁剪仅约束全文读取与自动续读，并在批次有序提交时重新计算并行读取的共同剩余预算。

- 身份从同一份不可变内容生成。inspect/range 只对不超过 256 KiB 的磁盘文件捕获全文身份；更大的局部读取仍为有界流式读取。显式 full 及其主机续读的磁盘快照上限为 64 MiB，避免无界内存；超出时仍可局部读取，但不能拼接成未经证明的全文覆盖，未完成的 full 转为 needs_scope。这是内部资源边界，无新增用户配置。
- 游标必须完整匹配主机最近签发的会话、代次、路径、快照、终点和位置；路径由真实 reader 解析，执行使用当前来源生成快照并核对。不能删除字段或重写位置来通过校验。
- `edit_file`、`multi_edit`、`delete_symbol`、`notebook_edit` 从真实 Preview 的最终修改推导原始所需范围；`write_file` 覆盖要求当前原始版本的全文证据；`delete_range` 继续由锚点审计拥有。`move_file` 不替换源内容且拒绝覆盖已有目标，不要求全文阅读。
- 同批读取不提供同批写入证据。依赖前一步新文本的一组修改使用 `multi_edit`，以最初版本统一预检；历史分立调用链的兼容测试显式启用回退模式。写入执行再次检查预检来源，防止预检之后新增的用户内容被覆盖。
- 未知范围写工具只受真实未解除义务约束；补齐前一轮证据后重新判断并解除历史阻塞。只读和不修改工作区的记账操作保持可用。
- 重建豁免绑定同一肯定重建语句中的完整目标路径，拒绝否定语句、同名不同目录及文件名子串匹配。
- 桌面和 CLI 保留不连续区间，使用一基行号并提供暂停后的操作说明；generation/sequence 拒绝过期更新，任务结束清除活动状态。

契约兼容：现有 `read_file` 的 intent/cursor 已属于本 PR 的模型可见 schema；更新 golden 固定该有意变化，升级首次请求可能重新建立工具前缀缓存。内部读取信封、原始来源摘要和写入预检数据不进入模型请求。不改变会话存储格式；旧游标在新运行中失效后须重新读取。真实 provider 成功率、token 费用及原生 WebView 验收仍须单独记录，不能由单元测试代替。

## 真实测试后的收尾修复

历史完成事实、当前请求可见原文、写入操作依据分别由协调器、请求可见索引和 writer 预检拥有。读取结果在有序 finalizer 内按 workspace、规范路径、来源类型、原始身份和快照关联任务；同版本复读保持已满足状态，扩大范围保留覆盖与预算，只补缺失区间。新回合使用新的注册表和游标绑定。

结构化 reader 不再使用通用字符串去重。仅当本轮实际请求仍包含字节一致的原始结果、且区间已经覆盖时才能返回引用；引用不计入新增交付，不允许引用链或同批结果充当模型已见内容。原文被投影移除或被扩展改写后重新交付必要窗口，不撤销过去的完成事实。扩展将正文替换成不可解析文字时也必须清除来源身份。

本轮新增可选 `read_pause` LocalOnly 记录及 `incomplete_read` 回合 outcome，补充上一节持久化边界：无存储迁移，旧会话缺失字段保持原行为，旧客户端通过既有 LocalOnly 工具标识忽略记录。摘要最多包含 32 个文件、每个区间字段 64 段，不保存正文或可执行游标；实时展示与历史回放按同一记录 ID 去重。状态是未完成暂停，不是成功或网络重试；用户通过现有输入框补充要求继续。该记录不恢复旧游标，也不授权写入。

保留原始 64 组真实测试，增加 32 组定向场景及 12 组精确写入测试。HTTP 层限制全部上游请求（含重试）为 600 次，每次预留 128,000 token，未知用量保留预留额，总计最多 300 万 token 或 4 小时。代理不存储凭据和请求正文。模型执行任务失败与宿主不变量失败分别统计。

本轮不改变工具 schema、系统前缀和默认配置；压缩后必要的原文重新交付可能增加单个请求输入量，须以实测报告说明，不宣称普遍降本。确定性测试、真实模型测试、原生 WebView 验证、远端 CI 和发版状态分别记录。
