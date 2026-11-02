---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-28
---

# Carried proof and file state / 恢复证明与文件状态

## Behavior / 行为

A pending Delivery change may carry verification, inspection and sign-off
across a Controller rebuild. Its fingerprint now includes regular-file bytes
and the full file mode reported by Go. Changing permissions without changing
bytes invalidates that carried proof and requires fresh evidence.

Delivery 待完成修改可在 Controller 重建后保留验证、检查和签收证明。指纹现在同时
绑定普通文件字节与 Go 返回的完整文件模式。只改权限、不改内容，也会使旧证明失效，
恢复后需要重新取得证据。

The existing content-version reader supplies the digest and detects observed
read changes. The proof owner additionally checks file identity, mode, size
and modification time around that read. It does not change permissions or
repair files. Absence has its own versioned fingerprint.

既有内容版本读取器提供摘要并检测读取期间观察到的变化；证明 owner 另外核对读取
前后的文件身份、模式、大小与修改时间。它不修改权限或修复文件。文件缺失使用独立
的版本化指纹。

Every nonregular path returns an empty fingerprint and never reuses carried
proof. This includes a symlink to an otherwise regular file, as well as
directories and special files. A dangling symlink is not absence.

The cost is fresh verification, inspection and sign-off after continuation or rebuild even
when a symlink and its target bytes have not changed. This invalidation does
not itself produce a separate warning. Existing tools can still collect fresh
evidence; tool permissions and normal completion rules are unchanged.

所有非普通文件路径返回空指纹，均不复用携带的证明，包括指向普通文件的符号链接、
目录与特殊文件；悬空链接不算文件缺失。即使链接及目标字节没有变化，继续执行或
重建后也要重新验证、检查和签收，这是明确的复用成本；失效本身不会单独显示警告。
仍可通过原工具收集新证据，不改变工具权限或正常完成规则。

## Persisted compatibility / 持久化兼容性

The existing `DeliveryCheckpoint.Proven.Paths` map remains a map of strings.
New values use the `file-state-v1:` prefix. No field, enum, RPC, configuration
or session format is added, and comparison does not rewrite the checkpoint.

既有 `DeliveryCheckpoint.Proven.Paths` 仍为字符串映射，新值使用 `file-state-v1:`
前缀。不新增字段、枚举、RPC、配置或会话格式，比较过程不改写 checkpoint。

| Fingerprint | Current reader | Previous byte-only reader |
| --- | --- | --- |
| Legacy raw SHA-256 or `absent` | Reads Goal data; requires fresh proof | Retains its previous behavior |
| Current regular-file state | Reuses only matching observed state | Reads Goal data; mismatch requires fresh proof |
| Current absence | Reuses only actual absence | Mismatch requires fresh proof |
| Unknown future value | Mismatch requires fresh proof | Mismatch requires fresh proof |

Legacy values cannot prove which permissions or file kind were checked.
They are deliberately not upgraded from current metadata: that would invent
past evidence. Existing goals and transcripts remain readable; after upgrade
or downgrade, a pending goal may need verification, inspection and sign-off again.

旧值不能证明当时检查过哪些权限与文件类型，因此不会用当前元数据自动补成历史
证据。既有 Goal 和会话仍可读取；升级或降级后，待完成 Goal 可能需要重新验证、
检查与签收。新旧程序共存不等于已解决其他持久化写入协议问题。

## Evidence and limits / 验证与限制

The Boot regression uses real file tools, a real verification command and
production Goal persistence. It closes one Controller, loads its session and
sidecar into a second Boot, then observes structured readiness audits after an
external permission-only edit. Unchanged and content-edited files are controls.

Boot 回归通过真实文件工具、验证命令及生产 Goal 持久化生成证明，关闭 Controller，
再用第二次 Boot 加载会话与 sidecar。外部只改权限后，测试检查结构化 readiness
审计，并保留未变更与内容变更对照。

Every arm still owes a high-risk review. The regression distinguishes which
verification, inspection and sign-off obligations return; it does not show an
otherwise complete Goal bypassing every finalization gate.

所有分支仍欠高风险审查。回归区分恢复后哪些验证、检查和签收要求重新出现，不把
这一局部证明错误描述为已经绕过所有完成门禁。

This is a scripted provider and two builds within one test process, not a
live incident, HTTP/browser test or actual process-death recovery. The Windows
inventory requires 14 named events across Agent and Boot without skips,
including parent events. Unix tests also refuse real links and a FIFO.

The shared `scripts/windows-concurrency-tests.mjs` step reads the package-qualified
`carried-proof-file-state.json` inventory under `.github/windows-concurrency/`.
It retains all 14 requirements and both package terminals; broad smoke is separate.

这是脚本 provider 与同一测试进程中的两次构建，不是线上事故、HTTP/浏览器测试
或实际进程死亡恢复。Windows 清单要求 Agent 与 Boot 两个包的 14 个命名事件无
skip 通过，计数包含父测试。Unix 测试另覆盖真实链接与 FIFO 的拒绝。

共享脚本读取 `.github/windows-concurrency/carried-proof-file-state.json` 中按包
限定的清单，保留全部 14 项与两个包终态要求；broad smoke 仍单独运行。

For regular files on Windows this contract covers content and the read-only bit
only. Go's synthesized mode does not establish POSIX permission semantics or
cover Windows ACLs and full security descriptors.

On any platform, Go file mode does not capture ownership or extended attributes.
The checks are observations, not an atomic
multi-file snapshot or protection against arbitrary concurrent external writers.
Rooted source authority, physical quiescence and durable publication remain separate.

Windows 普通文件合同仅覆盖内容与只读位；Go 合成的模式不代表 POSIX 权限语义，
也不覆盖 Windows ACL 或完整安全描述符。任何平台上的 Go 模式均不涵盖所有者与
扩展属性。这些检查是
观察，不是多文件原子快照，也不能约束任意并发外部写入者。根目录授权、物理静默
与持久发布仍由其他机制负责。

## Local cost observation / 本机开销观察

Three 200 ms benchmark samples on a Darwin/arm64 M1 Max produced these medians.
The old arm uses the exact preceding production owner with the same benchmark.
These are sequential local samples, not a production latency guarantee.

| File size | Byte-only owner | File-state owner | Allocated bytes, old / new |
| --- | --- | --- | --- |
| 4 KiB | 18.569 µs/op | 28.771 µs/op | 5,400 / 34,552 |
| 1 MiB | 616.436 µs/op | 535.338 µs/op | 1,057,306 / 34,558 |

Metadata checks add work for small files. The existing streaming content reader
uses bounded scratch space instead of allocating the whole regular file.
This does not establish a general speed-up or a bound for many-file recovery.

上述结果来自 Darwin/arm64 M1 Max 上三次 200 ms 采样的中位数，旧侧使用精确前版
生产 owner 与相同 benchmark。元数据检查增加小文件开销，流式读取减少大文件的
整块分配；这些本机顺序采样不构成普遍加速结论或多文件恢复时延保证。
