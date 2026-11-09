## 修复

- 修复工具注册表的读锁被重复获取、在另一处同时注册或移除工具时可能整体卡死的问题；此前启用 MCP 或延迟加载能力的会话偶尔会一直显示「任务正在运行」（发现于 d09a7c557c 的测试）。

---
<!-- lang:en -->

## Fixed

- The tool registry no longer takes its read lock twice while building a turn's tool list. A concurrent tool registration or removal could deadlock it, leaving sessions with MCP or deferred capabilities stuck on "task running" (found by the test in d09a7c557c).
