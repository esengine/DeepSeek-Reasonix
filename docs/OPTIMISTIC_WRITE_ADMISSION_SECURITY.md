---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-25
---

# Optimistic named writes / 乐观子任务具名写入

## Cause and owner / 根因与所有者

- The task registry skipped explicit-fence binding for whole-workspace claims,
  including optimistic claims. Child agents also lacked the shared scheduler;
  the parent-only admission explicitly skipped nonzero subagent depth.
- A real background task could therefore own a file while an optimistic sibling
  wrote it through either write_file or a dynamically resolved local capability.

乐观任务没有安装显式路径围栏包装器，子代理也没有共享调度器的实际调用准入。
真实后台任务持有文件分工时，乐观兄弟任务仍可通过直接或动态工具写入该文件。

## Repair and authority / 修复与权限

- The common child-options owner shares the scheduler. Concrete execution admits
  named writes after target resolution and permission, before pre-hooks and the
  actual call. The whole path set is reserved once and released by call cleanup.
- The root comes from that actor's existing grant. WriteWorkspaceRoot is not
  enabled merely for normalization: it also changes default observation scope.
- Parent admission and explicit child wrappers retain their existing owners.
  Existing wrappers are not double-reserved. Opaque legacy Bash/MCP/custom tools
  retain their policy and availability; the entire tool set is not fence-filtered.

统一子代理构造器传递同一调度器，在真实目标解析和权限检查后、hooks 和执行前申请完整
具名路径集合，并由本次调用清理释放。路径根取自该执行主体已有授权，不为锁而扩大
工作区观察范围。保留父代理、显式围栏和未知范围工具的既有分工与权限。

## Evidence / 证据

- Real boot.Build holds an explicit background task in its first model request.
  Task/fleet siblings use direct or capability writers on overlapping/disjoint
  files. Overlap is refused; disjoint writes finish while the holder is live.
- The compiling original owners write the held file in both direct and dynamic
  task cases. The corrected execution owner refuses them. Unit probes also
  require all-or-nothing multi-path admission and release while the actor lives.
- Real provider requests retain the same serialized tools and system prefix.
  Local and hosted platform/race evidence are recorded separately in the PR.

回归使用真实装配和通道控制后台模型等待，不靠瞬时竞争或私有字段伪造执行状态。
直接和动态 task 反例已在旧所有者上编译并实际写入占用文件；修复拒绝冲突，保留不相交
写入。另测多路径失败不留下部分占用、调用结束释放，以及实际模型前缀稳定。

## Limits and compatibility / 限制与兼容

- This is session-local legacy scheduling admission. It is not cross-process
  CAS, content-version enforcement, automatic isolation or the new transaction
  protocol. Older processes cannot gain these checks from this process.
- Explicit task-wide reservations, workspace lease retention, opaque hook effects,
  unknown write extents and external writers remain separate review units.
- No stored format, tool definition, wire type, permission grant, mode or default
  changes. Conflict uses the existing write.path_busy refusal and retry policy.
- Author assessment does not replace independent code-owner/security approval.

本批只修复既有模式下同会话的具名写入准入，不宣称完成跨进程 CAS、强制版本检查或
自动隔离。任务长预约、工作区长租约、未知 hooks/写入范围、外部进程仍待后续处理。
没有新增持久字段、工具定义、权限或默认策略；作者分析不能替代独立审查。
