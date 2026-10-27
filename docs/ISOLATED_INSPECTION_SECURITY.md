---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-25
---

# Isolated result inspection / 隔离结果检查

## Cause and repair / 根因与修复

A restarted store reconstructs a pending result by asking Git for its candidate
tree and diff statistics. Recovery previously treated an inspection error and an
empty result as the same cleanup condition. Cleanup forgot the durable marker
before attempting worktree removal, losing the result's discoverable identity.

重启后的存储通过 Git 重建候选树和变更统计。此前检查失败与无修改共用清理分支，
先删除恢复标记再尝试移除工作树。即使 Git 只是暂时不可用，原结果 ID 也会丢失。

Recovery now returns inspection errors before any cleanup. The lookup owner
reports the existing `isolation.unavailable` refusal with the result ID and
retained-files guidance. A caller can retry the same ID when Git is available.
Genuinely unknown IDs retain their existing refusal.

修复将检查错误直接返回，禁止错误触发清理。共用查询入口使用已有的不可用错误码，
说明结果 ID 和文件保留情况。Git 恢复后可用同一 ID 重试；不存在的 ID 仍按原规则拒绝。

## Inputs, authority and effects / 输入、权限与执行

- Result IDs and durable markers retain their existing workspace scope. This
  change adds no path grant, root, Git command or model-controlled execution.
- All three callers share the fix: apply path discovery, apply execution and
  explicit discard. An inspection error mutates neither the marker nor the
  store's recovered-entry map and does not start apply/discard cleanup.
- Inspection can still invoke the existing Git snapshot machinery. This is not
  a promise that arbitrary Git internals, external writers or commands are
  confined, atomic or side-effect-free.
- Error details come from the existing local Git operations. The durable JSON
  shape, tool schema, refusal vocabulary and compatibility format are unchanged.

本批不增加路径授权、Git 命令或模型可控执行点。三个调用入口均复用同一修复；
失败时不删除标记、不登记错误重建结果，也不开始应用或丢弃。检查仍使用原 Git
快照流程，不宣称其具备系统沙箱、全局原子性或消除外部写入竞争的能力。

## Evidence and remaining bounds / 证据与未完成边界

Regressions create real disposable Git worktrees, restart the store and remove
Git from the test process's PATH. Every caller must preserve marker bytes,
candidate content, workspace content and pending identity. Restoring PATH must
allow the same operation to complete using the original ID.

回归使用真实临时 Git 工作树，在存储重建后暂时移除测试进程的 Git 搜索路径。
测试要求恢复标记逐字节保留，候选内容和主工作区不变，原 ID 仍可发现；恢复路径
后同一操作必须成功。没有修改用户 Git 配置或运行生产资源。

Age-based expiration, successful-empty inspection policy, interrupted Finish,
versioned lifecycle records, recovery cancellation, journaled application and
cross-process settlement remain separate review units. This repair does not
claim to preserve results from an older process still using the old policy.

按年龄清理、成功但为空的判定、Finish 中断、版本化生命周期、恢复取消、整合日志
与跨进程整合互斥仍须分批处理。本批不能约束仍使用旧清理策略的旧程序，也不代表
完整恢复层已经验收。作者说明不能替代独立安全审查。
