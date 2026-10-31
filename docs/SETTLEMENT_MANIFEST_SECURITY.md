---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-25
---

# Settlement manifest codec / 整合恢复清单编码

## Scope and cause / 范围与根因

Current isolated-result cleanup can forget its publication before a failed Git
removal. A recoverable replacement needs immutable file images independent of
candidate lifetime, followed by a separate progress journal and cleanup receipt.
Tree object IDs alone do not retain those bytes.

This prerequisite defines and tests that immutable input's codec only. It has
no production caller, disk publisher, source writer, recovery action or UI entry.
Existing cleanup is unchanged and the known cleanup defect is not fixed here.

当前隔离结果清理可能先移除记录，再遇到 Git 删除失败。完整恢复需要独立于候选目录
寿命的不可变文件映像，以及后续进度日志和清理回执；只有树对象 ID 无法保留字节。

本批只定义并测试清单编码，没有生产调用、磁盘发布、工作区写入或恢复入口。
现有清理缺陷仍未修复，不将编码原语描述为完整恢复。

## Input and authority / 输入与权威

- A manifest includes a source fingerprint, result/session/execution/attempt
  identities, generation and sorted literal Git file spellings.
- Each before/after image retains raw bytes, a content digest and Git mode:
  absent, regular, executable or symlink. Missing and empty remain distinct.
- The manifest revision hashes the complete canonical encoding, binding modes,
  every image and execution identity, not only changed-file content.
- Decoding requires the expected identity and an expected revision supplied by
  the caller. It cannot silently refresh a preview to new content.
- Both directions reject unsupported modes, bad digests, duplicate paths,
  logical slash traversal, invalid UTF-8 identities and invalid link targets.

清单绑定来源指纹、结果、会话、执行、尝试身份和代次，并保留修改前后的原始字节、
内容摘要及 Git 模式。摘要覆盖整份清单，区分缺失、空文件、可执行文件与链接。

读取必须提供预期身份和已选定的清单摘要；变化后的内容不能自动取得原预览的授权。
这些输入必须来自宿主的可信所有者，不能从待校验记录自身重新计算后充当授权。

The source fingerprint and digests are unkeyed integrity references. They are
not authentication, OS process identity, a capability grant or proof that the
images came from an authorized snapshot. No source directory is opened here.

Path validation concerns logical Git spelling. Literal magic, quotes, newlines,
Unicode and backslashes remain data. A later publisher must resolve actual
platform identities and aliases under host-opened roots and acquire write authority.

The codec does not reject a `.git` component; this format check is not write
authorization. Before any production apply, the consumer must reject `.git`
components case-insensitively and deny resolved access to repository metadata.

That denial must cover the actual git/common directories, symlink and platform
aliases, and linked-worktree metadata reached outside the visible `.git` name.
A spelling-only filter cannot establish that boundary.

编码器目前不拒绝 `.git` 路径分量；格式校验不授予写权限。生产整合接入前，消费方
必须不区分大小写地拒绝 `.git` 分量，并禁止解析后的访问落入仓库元数据。
该边界必须覆盖实际 git/common 目录、符号链接、平台别名及 linked-worktree 元数据，
不能仅靠可见名称过滤。

Modes are logical Git modes. Native permission bits, ownership, ACLs and extended
attributes are not encoded. The future physical journal must retain the native
metadata its rollback promises, and refuse transitions it cannot materialize.

来源指纹与摘要不是身份认证、OS 进程身份、能力授权或快照来源证明。本批不打开源
目录。路径校验只约束 Git 逻辑名称；宿主根目录、平台路径、别名和权限须由发布层
重新核验，不能把清单内的路径直接当作已授权的文件系统路径。

本格式只保留 Git 模式，不保存完整原生权限、所有者、ACL 或扩展属性。后续物理
日志须保留回滚所承诺恢复的原生元数据，并拒绝无法安全实现的转换。

## Format contract / 格式约束

| Element | Version-1 rule |
| --- | --- |
| Version | Exactly 1 |
| Writer protocol | Exactly `per-result-fence-v1`; declaration is not enforcement |
| Identity | Source SHA-256; nonzero generation; four nonempty identifiers, each at most 256 bytes |
| Images | Git modes 0, 100644, 100755 or 120000; digest and bytes agree |
| Canonical form | Fixed JSON fields/order; sorted files; empty data normalized to null |
| Limits | At most 50,000 files; raw image/path budget and encoded record each at most 64 MiB |
| Revision | SHA-256 of the entire canonical record |

The raw input limit applies before JSON decoding. Encoding checks input budgets
before serialization and rejects an oversized encoded result. These bounds are
codec limits; they are not a claim that peak memory is exactly 64 MiB.

Known version-1 records must match their canonical bytes exactly. Duplicate keys,
case aliases, trailing values/whitespace and normalization cannot create another
interpretation. The golden fixture pins this machine-owned format across changes.

Unknown version/protocol/fields, including nested fields, are refused without
changing the supplied buffer. There is no rewrite or fallback to legacy
`entry.json`/`inspection.json`. Human reformatting also refuses; no live metadata
is converted to this format by this unit.

版本 1 使用机器生成的固定 JSON 编码，文件排序并统一空字节表示。读取先检查大小，
随后同时校验身份、预览摘要、内容摘要和规范字节，拒绝重复键、大小写别名或尾随值。

未知版本、协议或嵌套字段均保留输入并拒绝，不回退旧记录；人工重新排版也不接受。
64 MiB 和 50,000 文件是编码层限制，不代表峰值内存仅为 64 MiB。本批没有迁移旧数据。

## Compatibility and rollout / 兼容与接入

| Existing reader | Effect of this unit |
| --- | --- |
| Legacy entry/inspection readers | No new records are published; existing bytes unchanged |
| Accepted per-result fence reader | Still refuses any future settlement entry without parsing it |
| Future manifest consumer | Must provide independently selected identity/revision and enforce writer protocol |
| Unknown future format | Refuse; preserve input; never replace it with a guessed version |

The immutable manifest has no lifecycle state. Progress/terminal decisions need
a separate versioned journal bound to its revision. Adding mutable state to this
format or overloading ended-runner inspection version 1 is not an extension path.

Before any production publisher, every destructive sibling must honor the
accepted stable fence after OS ownership. Older programs that ignore it remain
outside the guarantee; writing a protocol string cannot constrain them.

清单不承载可变生命周期。进度和终态必须由独立版本化日志绑定其摘要，不在本格式中
追加可变状态，也不复用仅表示已结束任务的 inspection 版本 1。

接入生产前，所有破坏性入口须在 OS 操作所有权内遵守既有稳定屏障。旧进程不认识
协议的边界仍存在；写入协议名称不能让旧程序自动遵守。

## Evidence and remaining work / 验证与剩余工作

- Round trips preserve binary/CRLF content, empty/missing states, modes and link
  bytes. Returned data and caller ordering are independent of input buffers.
- Changed source/result/session/execution/generation/attempt, names, modes and
  before/after content change the revision; old previews and foreign identities refuse.
- Unsupported JSON, images, file counts and byte limits refuse. Literal
  filenames and the fixed version-1 fixture round-trip without reinterpretation.
- This is a new internal protocol. There is no preceding compilable codec API;
  deliberate unsafe codec controls are not preceding-version defect reproductions.

序列化验证覆盖字节、模式、身份、代次、预览失效、未知输入、资源限制和固定格式。
新协议没有可编译的旧 API；不把缺失符号当作旧实现的行为负例。

Still required: rooted durable publication, immutable byte lifetime on disk,
candidate-independent discovery, source authorization/CAS, actual-byte snapshot
fidelity, progress-before-write, conditional rollback, cleanup-pending receipts,
restart/process-death recovery, frozen validation and native integration evidence.

仍需实现根目录句柄内的持久发布、字节保留、候选目录外发现、授权与 CAS、真实快照、
写前进度、条件回滚、清理回执、进程崩溃恢复和冻结验证。本批作者说明不替代独立
安全审查，也不代表原大 PR 的所有告警已处理。
