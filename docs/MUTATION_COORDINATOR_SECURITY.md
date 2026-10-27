---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-24
---

# Mutation coordinator security review / 写入事务协调器安全审查

## Scope and decision / 范围与结论

This is the author's security analysis of the first review slice of #14133,
extracted from source commit `467a61668a0781cab171536158536283d931e4d9`.
It is not an independent approval or a disposition of that PR's CodeQL findings.

The slice adds `internal/state/mutation`, content-version reads, and the minimum
non-blocking `workspacelease` compatibility claim API. It introduces no production
callers, shell launcher, hooks, worktree integration, settings or default change.

这是 #14133 第一批改动的作者安全分析，不代表独立审批，也不代表原 PR 的
CodeQL 告警已解决。本批只增加事务基础库、内容版本读取和旧协议兼容申请接口；
没有接入生产调用者、命令执行、hooks、worktree 整合或设置，默认行为保持原样。

## Threat model and authority / 威胁模型与授权

The protected property is exclusion among cooperating Reasonix processes that
share one trusted state directory. A caller may request conflicting paths,
cancel, fail, panic in a callback, retain a stale input version, or die while
queued/active.

The coordinator must not grant partial sets, steal a live lease,
release a successor, silently accept stale versions, or erase unreadable records.

`New(root, stateDir, ...)` is a host API. Both arguments and admission callbacks
must come from the authorized host, not directly from model-controlled JSON.

`Intent.Paths` may contain authorized paths outside `root`: root is a resolution
base, not an access-control boundary. Canonicalization establishes identity,
**not permission**. The host must resolve the actual effect and complete grants
before acquisition.

`Options.AdmissionCheck` can reject admission under the
lease; omitting it does not create an implicit permission check.

保护对象是共享可信状态目录、遵守协议的 Reasonix 进程。请求可能冲突、取消、
过期或崩溃；不得部分占用、夺取有效租约、释放后继事务或删除无法理解的记录。

构造参数和回调必须来自已授权宿主。资源可以是工作区之外已获授权的路径：
路径规范化只确认身份，不授予读写权限。权限检查及完整影响范围仍由宿主负责。

Not protected: malicious modification of the state directory by the same OS
user, non-cooperating filesystem writers, arbitrary symlink/hardlink replacement
after validation, filesystem/OS failure, databases, network services, credentials
or ports. Advisory locks are not a sandbox.

Different state directories do not
coordinate even if paths happen to name the same file. Never remove lock files
to resolve contention. The state directory must not be exposed as an execution
workspace or be placed in an untrusted/shared writable directory.

Existing named-file hardlinks are checked with `os.SameFile`. Directory resource
sets do not enumerate aliases outside their trees. A directory lease therefore
cannot establish complete alias coverage; future opaque-write callers must
constrain such aliases or add conservative coordination before claiming it.

不覆盖同一系统用户恶意篡改状态目录、不遵守协议的写入者、校验后的任意路径
替换、系统故障及外部服务。不同状态目录不互相协调；不能删除锁文件制造并发。
状态目录不得作为执行工作区，也不得放在不可信的共享可写目录中。

## Data flows and checks / 数据流及检查

| Input / 输入 | Sink and guard / 访问位置及约束 |
| --- | --- |
| Host `stateDir` | `New` creates `mutations-v1` with mode 0700; records use 0600 and atomic replacement. Existing directory ownership/ACLs are a host precondition, not revalidated here. Windows modes are not an ACL sandbox. |
| Host root and intent paths | `resources` uses existing `CanonicalWritePath`, resolves existing ancestors, refuses ambiguous leaf links/Windows spellings, sorts and deduplicates. The locked validation repeats resolution and compares the complete set. It intentionally permits authorized extra roots. |
| Expected-content map | Keys must be absolute and covered by held resources. `heldWithin` accepts case-folded coverage only when the filesystem confirms the same existing prefix; conflict detection may conservatively over-serialize. `validateExpected` fully hashes inputs again after grant, regardless of the metadata-only waiting optimization. |
| Content read path | `ReadContentVersion` is a low-level arbitrary-path read API, not a permission boundary. It hashes regular bytes with cancellation, identifies absent/directory/link objects and rejects unsupported kinds. File identity/size/mtime changes during reads refuse. Link versions hash link text; mutation path resolution refuses ambiguous leaf links. A directory version is not a recursive snapshot. |
| Transaction filenames | IDs originate from `crypto/rand.Text`, not session/execution/call IDs. Registry filenames come from directory entries, whose path components cannot contain separators. A loaded record must match its filename, supported resource schema and nonzero queue order before any stale-owner removal. Diagnostic identities are serialized as JSON, never evaluated as commands. |
| Capacity identity | Opaque bounded strings, positive units/limits, no duplicates; inconsistent limits refuse. IDs do not become filesystem paths. Admission is atomic with paths; path-blocked requests do not consume execution slots. |
| Legacy claims | `TryClaimPaths` rejects empty/unresolvable sets, uses the existing workspace and claim locks, publishes all paths as one claim and returns an idempotent release. Probe requests publish nothing. Failure/panic before handoff removes only the newly created claim. |
| Continuation | Copies resource identities into a fresh acquisition; it does not transfer a live lease or reuse expected versions/approval. The future caller must re-establish its authorization and content preconditions. |

上表逐项区分宿主路径、预期版本、随机事务身份及容量身份。文件访问 API 并不
自行形成权限边界。等待期间的元数据优化不能替代获得租约后的完整内容校验。
目录版本不是递归快照，续跑对象也不携带旧授权或旧审批。

## Ordering, liveness and failure / 顺序、存活及失败

The registry guard covers queue inspection and publication, not tool execution.
Compatibility discovery runs outside it and is followed by a locked recheck.

Earlier overlapping requests retain priority; disjoint resources may proceed.
Read-only/external effects cannot smuggle paths or expected versions through an
empty set. Capacity-only leases cannot authorize file mutations.

A unique live OS lock, not a PID or elapsed timeout, establishes transaction
liveness. A wait timeout abandons the waiter only. Process death releases OS
ownership; a later registry pass removes a structurally valid dead record.

Unknown/corrupt schemas refuse and retain the bytes, even if apparently stale.
No migration or repair is attempted.

Nested leases cannot widen their parent's
resources or end it. Duplicate `End` calls cannot release a successor's random
transaction identity.

`HoldUntil` delays release until all registered physical
execution completions close; a caller that forgets completion can block progress.

登记临界区只保护排队及发布。旧协议检查在临界区之外运行，随后再次校验。
超时仅终止等待；真实存活由 OS 锁判断。未知版本及损坏记录保留并拒绝继续。
嵌套调用不能扩大范围或释放父租约；重复结束不能释放后继事务。实际执行尚未
结束时必须登记完成信号，否则不能声称取消已安全释放写入权。

This layer does not perform writes on the caller's behalf, maintain durable
write preimages, or recover partially changed files. `Outcome` is the caller's
reported classification, not proof of rollback, commit or verification. A future
runtime/integration PR must establish those additional contracts explicitly.

本层不代写文件，不保存写前映像，也不恢复部分变更。结果枚举是调用者报告，
不是提交、回滚或验证成功的证明；这些保证属于后续运行时及整合层。

## Compatibility and security gates / 兼容及安全门禁

| Contract / 协议 | Existing behavior / 旧行为 | This slice / 本批行为 |
| --- | --- | --- |
| Legacy workspace claims | Existing long-lived owners and optimistic bypasses | Format unchanged; independent short claims interoperate when callers opt in. An old bypass cannot be repaired by this library alone. |
| Mutation record v1 | Absent on older studio | Path-only records; live/dead checked using OS lock. |
| Mutation record v2 | Unsupported by v1-only readers | Carries capacities; unsupported versions fail closed and retain records. No active mixed-version safety claim beyond these readers. |
| Settings, transcripts, results and wire | Existing schemas | Untouched; no new defaults or automatic migration. |

Required review includes actual first-slice CodeQL results; inherited #14133
alerts are not blanket false positives. This slice has no production command
execution sink. The two old command-sink discussions remain obligations of the
later shell/hook PRs.

Before runtime activation, independently review all effect
resolvers, access checks, snapshot/version provenance and physical completion
callers; passing this library's tests is insufficient for that activation.

必须核对本批实际 CodeQL 结果，不能把原 PR 的告警批量认定为误报。命令及 hook
执行的数据流由后续对应 PR 单独审查。接入运行时之前，还必须审查影响解析、
权限、版本来源和实际执行完成回调；基础库测试通过不代表上层已安全接入。

## Verification map / 验证对应表

- Complete-set cancellation and no widening: `coordinator_test.go`,
  `capacity_scope_test.go`, `try_test.go`.
- Conflict fairness and independent progress: `fairness_test.go`,
  `capacity_test.go`, `compatibility_phase_test.go`.
- Stale reads, aliases and file identity: `stale_wait_test.go`,
  `case_fold_test.go`, `workspace_resources_test.go`, `content_version_test.go`.
- Refusal/panic and completion ownership: `callback_failure_test.go`,
  `completion_test.go`, `workspacelease/claim_panic_test.go`.
- Real subprocess death and protocol preservation: `process_test.go`,
  `capacity_process_test.go`, `compatibility_test.go`.

上述测试分别验证完整资源申请、公平排队、旧视图拒绝、别名身份、异常清理、
实际执行完成、真实进程退出及未知协议保留。平台测试和 CI 结果在每个 PR 中
按实际来源报告，不继承完整实现的旧验收结论。
