## 移除

- 移除任务预算设置 `task_cost_budget`、`task_time_budget_minutes`、`task_token_budget`：任务不再因为花了多少而被截停，老配置里的这些键启动时会自动清理并提示一次（a341d5615）。

---
<!-- lang:en -->

## Removed

- Removed the task budget settings `task_cost_budget`, `task_time_budget_minutes` and `task_token_budget`: a task is no longer stopped by what it spends, and old configs carrying them are cleaned on start with a one-time notice (a341d5615).
