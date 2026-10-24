---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-24
---

# Concurrent mutation review series / 并发写入分批审查

## Purpose / 目的

The complete implementation remains pinned at
`467a61668a0781cab171536158536283d931e4d9` in #14133 as a source reference.
It is being split following the maintainer's review-size request.
The source PR's validation is historical evidence, not qualification of a slice.

完整实现保留在 #14133 的上述提交。本系列按维护者要求拆分；原实现的测试记录
保留，但每一批都必须独立构建、检查及测试，不能继承整体分支的通过结论。

## Sequence and review boundaries / 顺序与边界

| Order / 顺序 | Review unit / 审查单元 | Prerequisite and activation / 前置条件及接入 |
| --- | --- | --- |
| 1 | Mutation admission primitive, content versions, legacy claim compatibility | Current slice; library only. No tool/controller/settings activation. |
| 2 | Tool effect descriptions and per-actor read-version provenance | Depends on 1. Review path grants, approval ordering, overlays and same-file stale-view regressions. |
| 3 | Shared tool/agent transaction assembly | Depends on 2. Review parent/child/fleet/dynamic paths, refused preparation and physical completion. |
| 4 | Source snapshots and private worktree preparation | Independent platform layer where possible. Review Git common-directory operations, modes, symlinks, untracked and ignored inputs. |
| 5 | Journaled per-file application and conditional recovery | Depends on 1 and 4. Review manifest containment, preimages, identity, failure injection, conditional rollback and idempotence. |
| 6 | Frozen check inputs and verification receipts | Review input observation, environment identity, stale evidence and unavailable backend behavior before integration activation. |
| 7 | Durable isolated task records and lifecycle | Depends on 4 and 6. Split preparation/execution from recovery/retention if either diff becomes too broad. |
| 8 | Task/fleet/background and nested ownership | Depends on 3 and 7. Review budgets, continuation, direct-parent identity, cancellation, restart and cross-version fences. |
| 9 | Root conversations and source integration | Depends on 5–8. Review current grants, source drift, controlled checks and recovery blocking together. |
| 10 | Controlled portable verifier and confined shell/hooks | Separate written command/authority security review; no unsupported-platform enforcement claim. |
| 11 | Controller/wire/frontends and migration/default selection | Activate only after required lower-layer review and platform evidence; preserve legacy settings and active runtime protocols. |
| 12 | Product/process performance and final acceptance | Reconcile every source delta, tests and compatibility evidence on the combined accepted tree. |

表中是依赖关系及审查边界，不是承诺将每行所有文件一次性提交。若一行仍然
包含多个独立机制，应继续拆分。前置接口经审查确定后，再提交对应的接入批次。
优先使用直接面向 `studio` 的独立 PR；需要堆叠时明确实际基分支及依赖。

## Extraction and security rules / 拆分与安全规则

1. Each slice MUST build and run its own tests without unsubmitted sibling
   patches. No lint baseline, safety gate or coverage floor is relaxed to make
   an extraction pass.
2. Every PR MUST state Cause, Blast radius, Neighbouring behaviours tested and
   Why this layer, plus its actual base/head and local/hosted evidence.
3. Sensitive slices MUST document input provenance, authority, filesystem or
   command sinks, validation, failure recovery and explicit unenforced bounds.
4. Original CodeQL findings remain unresolved until assessed at their actual
   sources and sinks. Moving code between PRs is not security remediation.
5. The source branch MUST remain recoverable. Each extracted file or hunk is
   accounted for as kept, adapted, deferred or intentionally dropped; the
   combined final tree is compared with the pinned source plus upstream changes.
6. Primitive API simplifications in slice 1 are intentional: use `Options`,
   lease methods and explicit-context version reads. Runtime-only context
   conveniences and legacy recovery admission hooks are deferred to their users.

每批必须独立验证，不降低门禁；安全说明不能用“旧分支测过”替代。拆分时保留
完整来源，并记录适配或延期的差异；最后对合并后的代码树做完整性核对。第一批
去掉尚无调用者的便利函数，后续接入时按已审查接口适配，不复制旧 API。

## First-slice review / 第一批审查入口

Read [the coordinator security review](MUTATION_COORDINATOR_SECURITY.md), then
`internal/state/mutation/doc.go`, `coordinator.go`, `registry.go`, `resources.go`,
`capacity.go` and `completion.go`. The compatibility change is limited to
`workspacelease/claim_attempt.go` and its existing `paths.go` publication owner.

先阅读安全审查及包说明，再审查资源申请、登记、身份、容量及释放。旧协议的
调整限于独立短申请及其发布清理，不接入运行时恢复屏障或修改旧锁语义。
