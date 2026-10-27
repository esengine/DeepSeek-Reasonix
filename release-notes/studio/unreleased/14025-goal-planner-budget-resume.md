Goal 预算耗尽后恢复时，规划与执行一起获得新切片。

## 修复

- 双模型 Goal 在显式预算耗尽后恢复时，也重置工具 planner 的本地支出，避免旧支出提前结束新规划。关联 #14025。
- 保留累计统计和交付证据；手动暂停、其他停止原因与拒绝恢复不重置 planner 预算。关联 #14025。

---

<!-- lang:en -->

## Fixed

- Resuming a dual-model Goal after an explicit spend-budget stop also resets its tool planner's local spend, so old spend does not truncate renewed planning. Refs #14025.
- Cumulative statistics and delivery evidence remain. Manual pauses, other stop reasons and refused resumes preserve planner spend. Refs #14025.
