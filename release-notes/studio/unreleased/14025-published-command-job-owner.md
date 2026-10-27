Windows 跟踪命令保留已发布的 Job Object owner。

## 修复

- 重复取消和收尾时保留原一次性 owner，避免重新调用按 PID 寻址的停止路径。关联 #14025。
- 成功分配 Job Object 后不再启动额外 PID 扫描器。关联 #14025。
- 无 Job Object、未跟踪命令及独立树追踪的旧边界仍在，不代表完整进程隔离已完成。关联 #14025。

---

<!-- lang:en -->

## Fixed

- Tracked Windows commands retain their published Job Object owner across repeated cancellation and reaping, avoiding a new PID-based stop attempt. Refs #14025.
- A successfully assigned Job Object replaces the auxiliary PID walker. Refs #14025.
- Existing non-job, untracked and standalone tree-tracking limits remain; full process isolation is not established by this change. Refs #14025.
