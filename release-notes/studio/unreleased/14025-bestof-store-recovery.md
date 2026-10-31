Best-of 候选现在保留统一 Store 的原结果 ID，可在失败与重启后继续定位。

## 修复

- 候选写入后发生 panic 或运行错误时保留结果并停止自动评选和发布；未知执行范围不会被当作空结果删除。关联 #14025。
- 记录创建、发布与清理阶段，支持按原 ID 恢复已完成候选，以及重试已提交的清理；清理重试不重复写入工作区。关联 #14025。
- 保持既有工具启用条件和权限；未知执行范围仍需 host 完成证明，不把进程退出推定为物理静默。关联 #14025。

---

<!-- lang:en -->

## Fixed

- Keep best-of candidates under their original Store result IDs after runner errors or panics, and stop automatic judging/publication while execution is unknown. Refs #14025.
- Record creation, publication and cleanup progress so completed results and committed cleanup can resume by the same ID after restart, without republishing during cleanup. Refs #14025.
- Preserve existing tool settings and permissions; unknown execution still requires host completion evidence rather than inferring physical quiescence from process exit. Refs #14025.
