# 读取证据生命周期

分页、源版本观察和写入授权是不同的事实。本次承接 #9966/#9992 的读取证据工作，
并处理 #9994/#9995 的重复编辑报告，不放宽权限或沙箱边界。

## 运行时归属

| 归属 | 契约 |
| --- | --- |
| `readcoord` | 记录范围、源快照、EOF 和续读预算。普通 `inspect`/`range` 缺口不阻塞终答；已附加的 Stop 仍会阻塞。 |
| 观察账本 | 记录已送达行哈希及序号。同批次读取不授权写入。旧观察早于上次写入时，去重逻辑重新送达原文。 |
| 操作守卫 | 冻结每个被拒操作的目标、版本、范围/哈希和 provider 边界，不再重放旧替换参数来清除拒绝状态。 |
| 操作账本 | 端到端拥有一次意图变更：支撑它的读取、落地它的写入、结算它的验证，以及失败时的有界恢复预算。 |

## 操作生命周期

操作身份来自这次调用要做什么——真实目标与参数，而不是 provider 每轮的 call ID，
因此同一处编辑换新 ID 重新提交仍然可被识别为同一个操作。状态为 `prepared`、
`applied`、`verification_pending`、`settled`、`failed`、`unknown` 和 `needs_user`；
`settled` 与 `needs_user` 是终态，不会二次转移。普通任务按真实工具结果结算；只有
交付底线会让一次变更保持开放，直到覆盖其路径的验证通过。

同一个操作以相同原因失败两次是循环，不是自我纠正：宿主停止提供自动恢复，将其
置为 `needs_user`，并拒绝再次执行它。只有真实的新信息才会开启新的恢复轮次——源
版本变化、新证据、成功写入使原源失效，或用户开启新一轮对话。只有宿主拒绝会消耗
这份预算；真正执行并报告失败的工具属于信息，仍由既有的重复失败与循环守卫处理。

## 回执与源令牌

每个成功的工具结果都会带上宿主自己的回执 ID（`[receipt r_1a2b3c4d]`），
`read_file` 结果带上它的源令牌（`[source_token r_…]`，即同一个 ID，用作它所展示
版本的句柄）。`complete_step` 接受 `receipt_ids`，`edit_file`、`write_file` 与
`multi_edit` 接受可选的 `source_token`。

引用 ID 是精确的；匹配模型重新键入的命令文本不是：少一个 `cd` 前缀、换一种引号
风格、调换一个参数顺序或换一个工作目录，都会让真实跑过的验证被拒。命令文本仅作
为未提供 ID 时的兼容路径保留，其余场合只用于展示。

被引用的源令牌会与宿主自己的记录核对。指向另一个文件、更旧的快照、只覆盖本次写入
所替换内容一部分的窗口，或宿主从未签发过的令牌，都不构成证明，会带着具体恢复动作
被拒绝一次。引用是可选的：不带令牌时既有的快照匹配仍然权威，普通的“读后编辑”路径
保持不变。

只有显式工具参数 `intent=full` 建立全文终答约束。不机械验证自然语言声称，也不
解析其生成新约束；普通终答成功不是全文审查证明。全文要求必须到达已验证 EOF，
或者在既有预算内暂停。定向搜索/读取策略回执不能证明全文已读完。

原要求被证据满足、新观察确认不同版本或不存在、或者完成的写入使旧操作失效时，
结束旧 requirement。未来每次写入独立验证当前目标。预检缓存键包含调用 ID、
工具名、参数及观察边界，随批次结束清理；延迟清理只移除完全相同的 requirement key。

## 写入边界

- 编辑使用实际预览的受影响范围和源身份，执行时再次核对。没有完整版本身份的
  有界窗口可以用当前行哈希证明局部范围，但不能跨版本拼接或证明整文件覆盖。
- 全文件替换需要当前完整证据或既有宿主记录的重建授权。创建绑定“确认不存在”；
  预检后、执行前出现的文件不会被直接覆盖。
- 既有锚定删除保留 anchor audit，不新增 `delete_file`。移动保持原始字节，
  包括二进制文件：验证宿主观察的源身份，不要求文本覆盖。目标及平台移动检查
  保留原实现；不宣称在最后身份检查之后还能对任意外部写进程提供原子 CAS。
- 纯元数据形式的 `git commit -m` 不要求正文证据；可能修改正文的形式保持保守。
  共享分类器识别 `git --no-pager diff/status/log`；重定向、外部 diff 和任意
  `-c` 覆盖不获得只读豁免。
- 本次仅证明字面量 `echo`/`printf` 输出重定向的写范围。不相交目标不继承另一
  文件的阻塞。脚本、动态展开、通配符目标、命令链、hooks 和未知范围仍不透明。
- 缺失证据的预检拒绝允许同批次内已预检通过、不相交的单文件写入继续。已经执行
  的失败、hooks、不明确范围和有依赖的验证保留原有依赖屏障。

## 恢复与兼容

诊断使用 `READ_PARTIAL`、`READ_CURSOR_INVALID`、`READ_SOURCE_CHANGED`、
`READ_HARD_STOP`、`WRITE_EVIDENCE_MISSING`、`WRITE_EVIDENCE_STALE`、
`WRITE_TARGET_ABSENT`、`WRITE_TARGET_AMBIGUOUS`、`VERIFICATION_RECEIPT_MISSING`、
`VERIFICATION_RECEIPT_MISMATCH` 与 `OPERATION_NEEDS_USER`，携带可得的路径、操作、
版本/范围及恢复信息，不携带
文件正文。

拒绝是机器可执行的，而不是自然语言建议：它会列出真实存在的回执 ID、宿主接受的
封闭动作集合（`use_receipt:<id>`、`reread_target`、`run_verifier`、`mark_manual`、
`abandon_edit`）以及剩余重试预算。模型只需选择动作，不必猜宿主接受哪种写法。预算
耗尽后，该操作连同下一步动作（`continue_verification`，已暂停的则是
`resolve_with_user`）交给用户，而不是再退回给模型。

在交付底线之外，`complete_step` 是一条注释：evidence 可选，宿主无法确认的内容随
签收一并报告，而不是拒绝。参数结构仍然校验。宿主不认识的成功命令在两种模式下都
记为“未分类”——各项目本就通过 Makefile、包装脚本和私有脚本做验证——同时交付门禁
仍独立要求已识别的验证，变更才能终答。

| 数据 | 新版本读旧数据 | 旧版本读新数据 |
| --- | --- | --- |
| Read envelope v2 | 保留字段语义 | 协议不变 |
| 分页文本 | 同时接受旧尾标及 `PARTIAL view` | 仅显示文本 |
| 可选 `tool_diagnostic` | 缺失安全 | 忽略未知可选字段 |
| LocalOnly `read_completion` | 缺失安全，仅供诊断 | 既有孤立工具哨兵防止进入模型请求 |
| Read status verdict、pause code/snapshot | 原有 State/Reason 可用 | 忽略可选字段 |
| 被拒操作及预检缓存 | 新 Run 为空 | 不持久化、不迁移 |
| 操作账本、回执 ID、源令牌 | 按轮次作用域，新一轮为空 | 不持久化、不迁移 |
| `complete_step.receipt_ids`、写工具 `source_token` | 可选，不填保持原行为 | 忽略未知可选属性 |
| 诊断中的恢复字段 | 缺失安全 | 忽略未知可选字段 |

`partial_read_sufficient` 表示允许终答，不表示宿主验证了模型理解。原始会话中的
覆盖回执仅用于诊断，不能授权恢复后的写入；模型与压缩投影剥离这些字段。完整短
文件字节不变；部分结果及追加续读提示会变化。`complete_step` 不再要求 `evidence`
并新增 `receipt_ids`，文件写工具新增可选 `source_token`；这些 schema 变更在升级
时改变一次稳定系统前缀，会话内前缀仍逐字节稳定。

## 可观测性

状态转移会向愿意接收的 sink 发布无正文计数器：`operation_settled_total`、
`operation_needs_user_total`、`operation_recovery_attempt_total`、
`operation_duplicate_block_total`、`verification_auto_attached_total`、
`verification_unclassified_total`、`read_source_changed_total` 与
`complete_step_optional_call_total`。它们只携带宿主标识，不含路径、参数、命令或
工具输出。真正回答“这次改动是否奏效”的是：同一操作的平均恢复次数、`needs_user`
占比，以及未分类命令占比。

## 验证

回归涵盖三次连续编辑/重读/重试、旧锚点变化、确认删除、源版本/存在性竞态、
冻结批次边界、不相交写入、不透明 shell、部分/全文终答、Stop 优先级及元数据投影。
从 #9992 的测试改进的真实 Build 回归在临时仓库暂存文件、读取大文件、提交并验证
真实 Git commit。

本地测试、race、lint 和交叉编译应与远程 CI、Windows 原生交互及真实供应商资格
验证分开报告。
