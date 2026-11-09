## 变更

- Coalesce named-path wait observations within one process; actual admission
  retains fresh locked checks. No new workspace mode is activated. Refs #14025

- 合并同进程内具名路径事务等待期间的重复登记读取。实际准入仍执行原有锁内复查，
  保留独立取消、排队与兼容语义；本批不启用新的工作区模式。Refs #14025
