---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-25
---

# Isolation CI coverage / 隔离机制 CI 覆盖

## Gap and repair

An all-green CI summary did not mean the isolation owners ran on native
Windows. PRs used a smoke package list; the extra worktree step selected only
rollback tests. The PR race list also omitted both full owning packages.

Pure internal/runtime/isolation changes could additionally be classified as
platform-neutral because that package has no OS-constrained file. Its Git,
filesystem and process contracts are nevertheless platform-specific.

The path gate now names that owner explicitly. Windows PRs execute complete
isolation, worktree and mutation packages with JSON terminal inventory; Linux PR
race checks include all three owners. The full worktree run replaces the redundant
rollback-only step.

The mutation primitive originally passed PR checks without its Windows package
running. A subsequent full push exposed an exact-path case-folding defect and a
one-second test latency assumption. The package now joins the native inventory;
the case-folding product repair is reviewed separately in #14424.

## Actual matrix

| Surface | PR coverage |
| --- | --- |
| Platform decision | Isolation production/test paths request native legs; existing neutral/unknown-path policies stay intact |
| Linux and macOS | Existing full root suites, when the native leg is required |
| Windows isolation/worktree/mutation | Native uncached complete package runs, with all three package-pass terminals required |
| Windows file locks | Existing full filelock package in the broad smoke command |
| Linux race | All three complete owning packages join the existing concurrency suite |
| Windows inventory | Uploaded JSON includes individual outcomes and conditional skips |
| Full push suite | Existing full root Windows/race checks remain unchanged |

Windows owner tests use the established 18-minute package and 25-minute step
bounds, with -count=1 and package concurrency four. Product deadlines, the
broad smoke timeout, workflow permissions and runner labels are unchanged.
Record observed durations before making future capacity adjustments.

The inventory requires terminal passes for all three packages and ten portable
ownership/checkout cases. Package passes with those cases skipped cannot pass
the gate. An earlier failing step leaves the job failed; a missing upload never
establishes a passing owner result.

## Regression and security scope

Real path-gate tests fail on the preceding classifier for pure isolation paths.
The existing YAML test owner now also checks that unfiltered PR commands select
all three complete packages on native Windows and with race enabled. A focused
rollback command cannot satisfy that coverage contract.

The legacy-wait regression pins the waiting callback outside the registry lock,
then uses TryAcquire to check capacity without queuing. A wrong reservation fails
immediately. Pinning prevents a preparation retry's valid queue priority from
impersonating exhaustion. The suite timeout still detects a stuck process.

No production Go, data format, tool permission or default is changed. No new
secret, credential, privileged trigger or write scope is introduced. Artifact
upload reuses the existing pinned action and seven-day retention; its input is
only the disposable test inventory.

## Evidence limits

Workflow configuration and local tests do not prove a native run happened.
Read the actual checkout, job outcome and JSON before claiming platform evidence.
POSIX-only skips do not count as Windows behavior coverage; Windows amd64
functional tests do not prove Windows ARM64 or Windows race support.

Individual package passes do not establish the full concurrency/isolation plan.
Crash recovery, safe retention, snapshot-bound verification, compatibility,
performance and complete combined-tree acceptance remain separate requirements.

## 中文说明

此前 Windows PR 白名单没有执行完整的隔离与 worktree 所有者包，额外步骤只测了
回滚子集；Linux PR race 清单也缺少这两个包。纯隔离运行时代码还可能被误判为
平台无关，导致原生检查完全跳过。

现在平台准入明确包含隔离所有者，Windows 执行隔离、worktree、mutation 三个完整包
并要求各自成功结束；
原始 JSON 上传用于核对实际执行、条件跳过和故障。完整 worktree 测试包含原有
回滚用例，不重复运行子集。Linux race 同时加入三个完整所有者包。

mutation 的路径大小写缺陷由 #14424 单独修复。本批在登记锁外停住等待回调，再执行
不排队的额度准入，避免准备阶段重试的合法排队优先权造成瞬态忙；保留额度不应被耗尽
的断言，去掉任意一秒钟的机器速度要求。
原生清单同时要求这两个关键回归实际通过，跳过不能算验收。

沿用已有超时、并发数、权限、固定版本 action 和结果缓存规则，不改变产品行为。
本地命令、工作流配置和宿主 CI 是不同证据；实际原生结果必须按提交及平台核实。
