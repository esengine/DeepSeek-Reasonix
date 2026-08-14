# intelifar 受控 IP 任务助手验收索引

本目录保留面向小微企业版本的任务助手验收证据。该模块只处理已授权的文档、IP 资产、证据、关系和 Wiki 任务，不是 coding agent，也不是复杂文档编辑器。

## 业务链路

自然语言目标 → 请求与角色检查 → DeepSeek 生成最多 6 步计划 → 服务端白名单校验 → 当前权限下的只读领域工具 → 固定结果包 → 证据门禁 → 人工复核。

Wiki 草案只生成建议，不保存、发布或覆盖正式 Wiki。每次领域工具调用前都会重新确认账号、工作空间和角色状态。

## 截图

- `01-agent-workbench.png`：7 类常用任务与自然语言入口。
- `02-grounded-delivery.png`：步骤收据、证据覆盖、确定结论、待核实项和交付门禁。
- `03-source-backlink.png`：从任务结论的来源编号打开资产详情。
- `04-boundary-block.png`：代码、删除和自动发布请求在模型调用前被拦截。
- `05-agent-mobile.png`：移动端工作台。
- `06-final-delivery-structure.png`：最终代码、测试、文档和验收产物结构。

## 自动化结果

- `report.md`：真实登录浏览器链路，覆盖影响分析、100% 证据覆盖、来源回链、越界拦截、Wiki 草案不落库、审计链和移动端。
- `e2e-result.json`：离线确定性模型下的脱敏任务结果和步骤收据。
- `real-deepseek-report.md`：使用 `apikey.txt` 中的真实 DeepSeek 凭据完成的调用摘要；文件不包含密钥。
- `real-deepseek-task.json`：真实 DeepSeek 的脱敏任务、计划、结果、用量与事件收据。
- `acceptance-report.md`：功能边界、评分与剩余生产责任。

截图、JSON 和报告均不包含 MinerU/DeepSeek 密钥、登录密码或会话 Cookie。
