# Transcript v2 / 会话同步 v2

## Ownership / 状态归属

The session runtime owns one transcript publisher across controller rebinds.
Accepted business batches advance `commitSeq` even when they contain no visible
message. `durableSeq` describes the persisted prefix; display revisions and
attempt indexes never substitute for business sequence numbers. Completion is
published only after the terminal persistence barrier. Save failure retains
visible output and reports recovery failure.

Session Runtime 在控制器重建期间保有唯一发布者。不可见业务批次也推进
`commitSeq`；`durableSeq` 表示已落盘前缀。显示版本和采样序号不充当业务游标。
终态经过持久化屏障后才发布完成；保存失败保留输出并明确报错。

Follow registers its bounded subscription before freezing a snapshot. If the
captured business cut is ahead of disk, initial Follow flushes it before issuing
canonical history cursors; large accepted batches cannot fall outside both the
durable page and bounded display tail. Active prefixes cover the remaining view.
Disk/content reads and transport writes run
outside the publisher lock. Queue overflow, rewrite, runtime rebind, gaps and
invalid settlement require a fresh baseline; none submit a model request.

Follow 先登记有界订阅再冻结快照；若捕获的业务切面尚未落盘，首次 Follow 先完成
该切面的保存再返回历史游标，防止大批已接受消息同时落在持久化页与有界显示尾部
之外；活动前缀补齐剩余视图。磁盘／正文读取和网络发送不占发布锁。缓冲溢出、历史重写、运行
代次变化、缺帧或错误配对要求重新同步，不会重新请求模型。

Sampling uses stable message/attempt identities and ordered chunk indexes.
Committed messages precede settlement frames, which reference their business
sequence. Cancellation persists the displayed partial output as interrupted.
Crash recovery guarantees persisted content, not tokens that never reached disk.

采样使用稳定消息／attempt 身份和递增 chunk 序号。完整消息提交先于引用其业务
序号的结束帧。取消保存已显示的部分输出并标记中断；崩溃仅保证恢复已落盘内容。

## Client behavior / 客户端行为

Local and remote chat use the same Follow consumer, record store and natural-flow
view. Snapshot installation commits records, runtime, prompts and active prefixes
together before consuming suffix frames. Assistant nodes use message IDs; tools
use call IDs. Deferred bodies remain loadable even with empty previews. Connection
loss is independent of task completion, and no watchdog completes or reruns turns.

本地与远程共用 Follow 消费器、记录存储及自然流界面。快照一次安装消息、运行态、
待处理交互与完整活动前缀，再接纳后续帧。assistant 使用消息 ID、工具使用 call ID。
预览为空的正文引用仍可加载。连接中断不等于任务完成，watchdog 不终止或重跑任务。

History retains three adjacent pages of 32 messages by default. Reading older
pages isolates live output; paging forward or locating a message makes evicted
history reachable again. The turn rail describes loaded turns only; canonical
search/locate remains available for the complete history. Final identity, elapsed
time and distinct sampling/tool counts come from the backend turn. Imports that
lack count records do not invent counts.

默认保留相邻三页，每页 32 条。阅读旧页时隔离实时输出；向前翻页或定位消息可重新
访问已淘汰历史。回合导航栏仅展示已加载回合，完整历史仍可搜索定位。最终消息身份、
耗时及去重采样／工具次数来自后端回合；缺少计数记录的导入数据不虚构次数。

## Compatibility and change notes / 兼容与变更说明

| Boundary / 边界 | Behavior / 行为 |
| --- | --- |
| Desktop ↔ Serve | Both must advertise/support `transcript-v2`; old Serve gets an upgrade error, no legacy chat fallback. / 双端必须支持 v2；旧 Serve 提示升级，不回退拼接。 |
| Session log / 会话日志 | Existing encoding and event kinds unchanged; old files remain readable. / 编码与事件种类不变，旧文件继续可读。 |
| Derived history index / 派生索引 | Schema v8 adds turn summaries/counts; disposable index can be rebuilt from the unchanged log. / v8 添加回合摘要与计数，可由原日志重建。 |
| Provider boundary / 模型边界 | No message fields, tool schemas, prompt/context or compaction policy changes. / 不修改消息字段、工具 schema、提示上下文或压缩策略。 |
| CLI ledger | May remain internal; never supplies v2 chat coverage. / 可保留内部实现，不提供 v2 聊天覆盖游标。 |

Removed chat snapshot assembly/cache authority, post-completion full history
rebases, metadata/watchdog completion guesses and legacy protocol fallback.
Correlated capability audit rows are merged into tool details; source records
remain available to persistence/export. Model switching, approvals, questions,
background jobs, explicit automations and Goals retain their command paths.

移除聊天快照拼装缓存的权威地位、完成后全历史替换、metadata/watchdog 完成猜测及
旧协议回退。可关联 capability 审计行合并至工具详情，原记录保留用于保存和导出。
模型切换、审批、提问、后台任务、显式自动化及 Goal 保留命令入口。

Diagnostics contain protocol/epoch/revision/coverage/attempt counts and recovery
reasons, never message bodies. Windows Git Bash sandbox permissions and repeated
model visual refinement remain separate issues; v2 adds no arbitrary step cap.

诊断仅记录协议、代次、版本、覆盖、attempt 数量和恢复原因，不记录正文。Windows
Git Bash 沙箱权限与模型反复视觉调整是独立问题；v2 不增加任意步骤上限。
