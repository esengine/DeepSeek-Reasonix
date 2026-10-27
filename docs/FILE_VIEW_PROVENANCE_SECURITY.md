---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-24
---

# File-view provenance / 文件读取版本来源

## Cause and scope / 根因与范围

`read_file` previously rendered one read, then independently reopened the file
or editor overlay to record its overwrite guard. A later buffer version could
therefore be certified without reaching the caller. A page also certified the
unreturned tail; an oversized read removed a previous guard.

旧实现展示内容后再次读取文件或编辑器缓冲区来记录版本，因此可能记录代理没
看到的后续内容。分页会误更新整个文件的记录，超限读取还会清掉已有保护。

The fix observes the same decoded reader consumed by the renderer:

- A successful complete read from offset zero updates the digest, within the
  existing 8 MiB decoded-content bound.
- Partial, oversized and failed reads retain the earlier digest.
- A first incomplete successful read records that incompleteness; `write_file`
  requires a complete reread or a targeted edit instead of treating it as unseen.
- Capture is bounded and streaming; it does not drain an unreturned tail.
- A nil `FileViews` leaves the reader unwrapped.

修复直接观察渲染器使用的解码流。只有成功、从零开始、完整返回且不超过
8 MiB 的读取才更新摘要；分页、超限和失败读取保留先前记录。首次不完整读取
会记录其不完整状态，完整覆盖前必须完整重读或使用局部编辑。捕获过程有界，
不额外读完未展示的尾部；禁用保护时保持原读取路径。

## Authority and sinks / 权限与写入入口

The host still binds `FileViews` through `boot.bindFileViews` when changed-file
protection is enabled. The input boundaries remain:

| Input | Existing boundary |
| --- | --- |
| Disk reader | Path resolution, read confinement, network roots, forbidden roots and opened-file checks precede capture. |
| Editor buffer | The host's `FileOverlay` is consulted after its existing path checks. |
| Observation | A digest is stored under the resolved path; decoded bytes live only for that call. No write authority is granted. |

宿主仍按原配置绑定保护记录。路径解析、读取范围、网络根、禁止目录及已打开
文件检查继续在捕获前执行；编辑器来源也沿用原路径检查。该改动不增加权限，
记录仅保存解析路径对应的摘要，解码内容只在调用期间临时保留。

The consumer remains `FileViews.checkOverwrite` in `write_file`; this patch
changes which observation can refresh it. Existing edit/write routes, encoding,
permissions, hooks, approval and protection opt-out remain in their owners.

There is no new shell command, disk format, wire field, tool schema or prefix
mutation. See #14133 for the preserved complete implementation. This independent
slice does not activate the pending transaction or isolation layers.

消费方仍是 `write_file` 的覆盖检查；本批只修正更新记录的来源。编辑、写回、
编码、权限、hooks、审批及关闭保护的行为继续由原模块负责。不新增命令、
持久格式、协议字段或工具定义；事务、隔离层仍分批审查。

## Limits and review evidence / 限制与审查证据

The guard has explicit limits that need the later runtime and provenance slices:

| Boundary | Remaining limit |
| --- | --- |
| Atomicity | This is neither compare-and-swap nor a file lock. External writes can race after the overwrite check. |
| Actor ownership | Parent and child registries can still share a view record. |
| Never-seen files | Legacy overwrites without any prior successful read remain allowed. A successful partial read now requires a complete reread or targeted edit. |
| Model delivery | Downstream truncation or summaries are outside this tool-level receipt; rendered bytes are not proof of later model visibility. |
| Authorization | The guard grants no access and does not replace permission checks. |

这仍是过期视图检查，不是原子比较写入、文件锁或权限控制；外部写入仍可能与
检查后的写回竞争。完全未读过的文件沿用原覆盖规则。父子代理独立记录、所有
已有文件的强制读取，以及下游截断后的模型可见性，均留给后续批次；不能把本批
称为整套并发问题已解决。

Deterministic regressions reproduce a changing overlay, three paging shapes,
and loss of the old guard on oversized input. These cases fail on the unmodified
owner before the fix.

The real `boot.Build` regression follows the production path:

1. Read the whole file, then apply an external edit.
2. Read only the header, then attempt a stale overwrite.
3. Check retained user bytes and the refusal delivered to the provider.

Neighbour checks cover complete reads, empty files, final newlines, CRLF, BOM,
UTF-16, nested verbatim reads, first partial reads, streaming, own edits and the
protection opt-out.

确定性回归覆盖变化的编辑器缓冲区、三种分页及超限读取。真实装配测试在完整
读取后模拟外部修改，只重读首行再尝试覆盖，并同时检查用户内容和模型收到的
拒绝结果；旧实现会失败。相邻测试覆盖完整读取、空文件、换行、编码、嵌套
读取、流式读取、自身编辑及原有关闭保护行为。作者说明不等于独立安全审批。
