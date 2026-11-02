---
owner: @esengine
backup: @SivanCola
status: active
reviewed: 2026-10-28
---

# Ancestor continuation admission / 祖先续跑准入

## Behavior / 行为

1. Within one `SubagentStore`, continuing an ancestor transcript reserves its
   canonical nearest source and target parent session before searching for a
   current-session copy. The reservation lasts until the returned run releases.
2. A competing continuation for that pair is refused as already in progress,
   including before the first copy publishes its `running` metadata. It MUST
   NOT create another copy or make another provider request.
3. `continue_from` and the accepted legacy `fork_from` input share this owner.
   Referring to an older ancestor or its nearest lineage copy uses the same
   reservation. Different target parents and different sources remain separate.
4. Failed preparation and abandoned unpublished runs release their reservation.
   After a completed run releases, another continuation reuses its persisted
   copy. Direct current-reference continuation keeps its existing ref lock.

1. 同一 `SubagentStore` 在查找当前会话副本前，按最近祖先来源和目标父会话预留续跑归属；预留随返回的 run 一起释放。
2. 首份副本尚未发布 `running` 元数据时，同一组合的竞争续跑也会被拒绝；不得再创建副本或发起模型请求。
3. `continue_from` 与兼容的旧 `fork_from` 输入共用此归属。较早祖先及其最近副本归一到同一来源；不同目标会话、不同来源保持独立。
4. 准备失败或放弃未发布的 run 会释放预留。已完成 run 释放后，续跑复用原持久化副本；直接按当前副本引用续跑仍使用原引用锁。

`errors.Is(err, ErrAncestorCopyBusy)` identifies the refusal through wrapping.
TaskTool uses the existing `subagent.settling` answer code and preserves that
sentinel; callers do not classify error prose.

包装后的错误仍可用 `errors.Is(err, ErrAncestorCopyBusy)` 识别；TaskTool 使用既有
`subagent.settling` 回答码并保留此标识，不解析错误文案。

## Compatibility and limits / 兼容与边界

| Boundary / 边界 | Contract / 合同 |
| --- | --- |
| Storage / 存储 | No metadata, transcript, status or schema change. / 不改变元数据、转录、状态或 schema。 |
| Existing ambiguity / 既有歧义 | Multiple existing copies remain refused; no automatic selection or deletion. / 已有多个副本仍拒绝，不自动选择或删除。 |
| Scope / 范围 | One Store instance; existing process-local locking contract. / 仅同一 Store 实例，沿用进程内锁合同。 |
| Cross-version writers / 跨版本写入 | No cross-process or old-writer fence; old binaries retain their behavior. / 不提供跨进程或旧写入者隔离。 |
| Recovery / 恢复 | No resume of failed/interrupted children, durable reservation or restart recovery. / 不恢复失败或中断子任务，不提供持久化预留与重启恢复。 |
| Authority / 权限 | Tool permission, cancellation authority, provider prefix and tool schema are unchanged. / 工具权限、取消权限、模型前缀和工具 schema 不变。 |

## Validation / 验证

1. Store regressions hold an unpublished preparation, cover both input routes,
   nearest-source aliases, independent parents/sources and release after errors.
2. TaskTool regressions block the existing parent-destruction callback before
   `MarkRunning` writes metadata. A competing call is refused; the first call
   receives the real persisted source transcript and creates the only copy.
3. These are local Store/TaskTool tests with a scripted provider, not a live
   provider, full Boot, browser, crash/restart or physical-process-scope test.
4. The Windows workflow requires every named regression and the package terminal
   pass, with no skips. Local cross-compilation is not native Windows evidence.

1. Store 回归保留未发布的准备结果，覆盖两种输入、最近来源别名、不同会话和来源，以及错误后的释放。
2. TaskTool 回归在 `MarkRunning` 写元数据前阻塞既有父会话销毁检查回调。竞争调用被拒绝，首个调用读取真实持久化来源转录并创建唯一副本。
3. 以上是本机 Store/TaskTool 与脚本 provider 验证，不是线上 provider、完整 Boot、浏览器、崩溃重启或物理进程范围验证。
4. Windows 工作流要求全部指定测试及包终态通过且零跳过；本机交叉编译不等于 Windows 原生执行。

A second TaskTool regression requests background execution without a job manager.
Preparation succeeds, then normal completion abandons the unstarted run.
No test calls `Release` for it; a foreground retry proves admission was freed.

另一 TaskTool 回归在没有 job manager 时请求后台执行：准备成功后由正常结束路径
放弃尚未启动的 run；测试不调用其 `Release`，前台重试证明预留已释放。

The shared `scripts/windows-concurrency-tests.mjs` step reads additive,
package-qualified inventories under `.github/windows-concurrency/`. This suite
requires 17 named passes including parents. The broad smoke remains.

共享脚本读取 `.github/windows-concurrency/` 中按包限定的独立清单；此套件要求含
父测试的 17 个指定测试通过，保留 broad smoke。
