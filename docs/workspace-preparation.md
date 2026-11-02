---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-17
---

# Workspace preparation / 工作目录准备

## User flow / 用户流程

In a new conversation, **Configure workspace** offers **Current folder** and
**Isolated worktree** choices.

In Current folder mode, the branch picker switches local branches
immediately through the existing guarded branch API. Branches checked out in
another worktree remain visible with their occupied path and cannot be selected.

新会话中的「配置工作目录」保留「当前目录 / 独立工作树」双选项，并标明选中项。选择当前目录时，
分支选择器通过既有受保护接口立即切换本地分支；其他 worktree 占用的分支显示
占用路径，并禁止切换。

The same entry stays available after the first message and inside an existing
worktree. Its panel shows the full branch and folder path, and keeps branch
switching available. Creating another worktree starts from a new source session.

首条消息发送后和已有 worktree 内仍保留同一入口。面板显示完整分支名、目录路径，
并继续支持切换分支。需要另建 worktree 时，从源目录新会话开始。

The palette exposes search and branch choices immediately. Full paths are
collapsed by default and can be revealed or copied. Typography uses the existing
UI, metadata and helper tokens, shared with the preparation states.

面板直接展示搜索与分支列表，完整路径默认折叠，可展开或复制。创建、成功、失败和
取消页统一使用现有正文、元信息、辅助说明字号；完整诊断按需展开。

Worktree mode selects a local or remote-tracking starting ref without switching
the source checkout. Sending starts preparation from its committed version.
The preparation screen retains the task and attachment names, shows actual
host phases, and exposes details and cancellation.

Only a ready workspace can
receive the original task. Failure and cancellation preserve the draft.

worktree 模式可以选择本地或远程跟踪分支作为起点，不会切换原目录。发送消息时
基于起点的已提交版本创建目录。准备页面保留任务和附件名称，显示后端真实阶段，
提供详情和取消。目录就绪后才发送原任务；失败或取消时保留草稿。

## Operation contract / 操作协议

| Endpoint | Behavior / 行为 |
| --- | --- |
| `POST /workspace/prepare` | `{id, selection:{sourceRoot, ref, attachments?}}`; return current operation / 返回当前操作 |
| `GET /workspace/prepare/{id}` | Reconcile the same operation / 核对同一操作 |
| `POST /workspace/prepare/{id}/cancel` | Request cancellation; return current state / 请求取消并返回当前状态 |

The operation ID is generated before sending. An identical repeated POST returns
the same result; a different payload with the same ID is rejected.

A lost POST
acknowledgement is reconciled by ID before any new creation is allowed. The
per-runtime registry retains the latest 32 completed attempts in memory.

Unknown
IDs after restart or eviction require explicit reconciliation; the client never
silently creates a replacement.

发送前生成操作 ID。同一 ID 和相同参数重复提交返回相同结果；同一 ID 的不同参数
会被拒绝。创建请求响应丢失时先按 ID 核对，不会静默重复创建。每个运行时在内存中
保留最近 32 次已完成操作；重启或淘汰后未知的 ID 需要人工核对。

Host phases include `validating`, `creating`, `checking_out`,
`copying_attachments`, `binding`, `cleaning`, and `cancelling`. Terminal phases
are `ready`, `failed`, `cancelled`, and `cleanup_failed`.

Error identities use
typed `workspace.*` codes. The frontend additionally represents unreachable
status and a published workspace retained after a late cancellation.

后端阶段包括校验、创建、检出、复制附件、绑定、清理和取消中；终态包括成功、失败、
已取消和清理失败。错误通过 `workspace.*` 类型码传递。前端另行展示状态不可达，
以及取消晚于目录绑定时保留已创建目录的状态。

## Ownership and bounds / 所有权与时限

Cancellation and runtime publication share one commit boundary. Cancellation
before publication stops construction and rolls back only the unpublished
worktree and its unchanged task branch. `cancelled` is acknowledged only after
cleanup.

If publication wins, the workspace stays ready; a user cancellation
still prevents automatic task submission. Cleanup uncertainty blocks replacement
creation and exposes diagnostics instead of claiming success.

取消与运行时发布共用一个提交边界。发布前取消会停止构建，只回收尚未发布的
worktree 及其未变化的任务分支；清理完成后才确认「已取消」。

发布先完成时保留
已绑定目录，但用户取消仍会阻止自动发送任务。无法确认清理结果时禁止重复创建，
并展示诊断信息。

The UI shows a long-wait notice after 15 seconds without inventing a percentage.
The host operation is bounded to 10 minutes; checkout commands to 5 minutes;
rollback to 1 minute. A timed-out operation becomes retryable only after cleanup.

Git cancellation terminates the checkout process group. Runtime assembly follows
request cancellation until successful construction, then transfers its lifetime
to the controller. Changing panes or workspaces invalidates old client attempts.

等待超过 15 秒展示长等待提示，不伪造百分比。后端操作最长 10 分钟，Git 检出最长
5 分钟，回收最长 1 分钟；超时清理完成后才允许重试。取消 Git 操作会终止检出进程组。

运行时构建阶段响应请求取消，成功后生命周期转交给控制器。切换页面或工作区会
使旧前端操作失效。

## Cleanup recovery / 清理失败后的恢复

`cleanup_failed` deliberately blocks another preparation in the same runtime.

1. Save the task and attachments, and reveal or copy the retained path.
2. Reconcile that directory and task branch using Git in a terminal. Preserve
   any external changes.
3. Once reconciled, choose **Close conversation** from its menu and start a new
   conversation in the source folder to retry. Closing retires the old runtime
   and its operation registry.

`cleanup_failed` 会阻止同一运行时再次创建目录。先保存任务与附件，展开或复制保留
目录的路径，再在终端使用 Git 核对该目录与任务分支，保留外部产生的修改。确认处理
完成后，在会话菜单中选择「关闭会话」，再从源目录新建会话重试；关闭会话会退役旧
运行时及其操作记录。

## Regression evidence / 回归验证

Regression coverage lives in `workspace_preparation_test.go`,
`workspace_prepare_test.go`, `build_lifetime_test.go`, worktree `cancel_test.go`,
and the frontend worktree composer and hook tests.

These cover publication versus
cancellation, cleanup before acknowledgement, retry after failure, lost
acknowledgements, real checkout descendant termination, and stale pane results.

上述测试覆盖发布与取消的先后顺序、清理后确认、失败重试、响应丢失、真实 Git 检出
子进程终止，以及旧页面结果失效。真实浏览器验证使用临时仓库与独立 Reasonix
配置目录，模型回复由本地测试服务提供；不代表真实模型或发布安装包已通过验证。
