---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-20
---

# Tool results and remote task recovery / 工具结果与远端任务接管

## Result retention / 结果保留

- A text-only MCP result is read exactly as before and never written to disk.
- A rich result (non-null `structuredContent`, or any non-text content) is kept
  whole in a private, versioned `.tool-result` sidecar, up to
  `tool.ResultRetainLimit` (16 MiB). A larger one is not kept, and the result
  text says so.
- The model sees the result's text, its `tool-result:` reference and the names
  of its top-level structured fields, never the structured values; they are
  read back with `read_file`.
- Content-addressed filenames keep a reused call ID from overwriting an earlier
  result. Transcript, provider tool schemas and system prompt are unchanged.

纯文本 MCP 结果保持原样且不落盘。富结果（含 `structuredContent` 或非文本内容）完整保存在 0600、
带版本的 `.tool-result` 中，上限 16 MiB，超限不保存并在结果文本中说明。模型看到文本、引用和
结构字段名，不直接看到结构值，需要时用 `read_file` 读取。

The model receives a stable `tool-result:<digest>` reference. `read_file` resolves
it only within the current session, including after temporary artifacts migrate
to durable storage. Reading a retained result preserves its external-data label.

模型收到稳定的 `tool-result:<digest>` 引用，`read_file` 仅在当前会话范围内解析，临时结果迁移到
持久化目录后仍有效。再次读取保留结果时，仍标记为外部数据。

## Accepted remote work / 已接收的远端工作

A modern MCP task is checkpointed before polling, under its host job ID. The
immutable `.remote-task` file contains the server, original task ID, protocol,
absolute deadline, and a digest binding the configured endpoint and credentials.

It contains no submission arguments or credential values. Closing the controller
detaches remote work; explicit stop retains cancellation intent separately.
Cancellation remains cooperative and does not prove that an external effect was
undone.

现代 MCP 任务在首次轮询前持久化，沿用宿主 job ID。不可变的 `.remote-task` 文件记录
服务、原始任务 ID、协议、绝对截止时间及服务地址与配置凭据的绑定摘要，不保存提交参数或凭据明文。
关闭控制器会分离远端工作；主动停止另行保存取消意图。取消采用协作机制，不代表外部效果已撤回。

Recovery requires session ownership and the currently enabled, authorized server
with a matching binding. Named hard-deny rules still apply. Recovery queries the
original task and never calls `tools/call` again.

A durable terminal receipt is replayed locally. Missing servers, changed identities, unknown versions, lost
tasks and transport failures remain visible; an unknown remote state is never
reported as successful generation. The deadline is not renewed on restart.

恢复要求当前运行时拥有会话，且服务仍启用、已授权、绑定摘要一致；工具级硬拒绝规则仍生效。
恢复只查询原任务，不重新执行 `tools/call`。已持久化的终态在本地重放。服务缺失、身份变化、
未知版本、任务丢失及传输失败都会显式报告，不把远端未知状态当成生成成功。重启不延长截止时间。

There is no automatic resubmission when a submission response is lost before a
task ID is received. Plain synchronous MCP calls and legacy background records
keep their existing behavior. Restoring an artifact record does not imply a
specific video service supports persistent tasks across its own restart.

提交响应丢失且尚未收到任务 ID 时，不会自动重提。普通同步 MCP 调用和旧后台记录保持原有行为。
宿主可恢复任务记录，不意味着特定视频服务一定支持其自身重启后的任务持久化。

## Compatibility / 兼容性

| Format / 格式 | Existing data / 旧数据 | New reader / 新读取器 | Previous reader / 旧读取器 |
| --- | --- | --- | --- |
| Session and job metadata / 会话与 job 元数据 | Unchanged / 不变 | Existing behavior / 原有行为 | Existing behavior / 原有行为 |
| `.tool-result` | Optional / 可缺省 | Version 1, lossless envelope / v1 完整返回 | Ignored by job enumeration / job 枚举忽略 |
| `.remote-task` | Optional / 可缺省 | Version 1, immutable identity / v1 不可变身份 | Ignored; no automatic takeover / 忽略，不自动接管 |
| `.remote-terminal`, `.remote-cancel` | Optional / 可缺省 | Local completion or cancellation intent / 本地终态或取消意图 | Ignored / 忽略 |

Unknown versions remain on disk without execution or normalization. Downgrading
can show a remote job as interrupted; upgrading can reconcile its retained
checkpoint. Deleting a session deliberately removes its sidecars too.

Runtime
status and artifact references append to tool results, never to the stable
system or tool-definition prefix. Cache acceptance is measured at the actual
provider request boundary.

未知版本保留原文件，不执行或改写。降级可能把远端 job 显示为中断，升级后可通过保留的检查点
继续核对。删除会话会一并删除其附属文件。运行状态与结果引用只追加在工具结果中，不进入稳定的
系统提示词或工具定义前缀；缓存验收以实际 provider 请求边界为准。
