# 真实供应商 E2E 重试记录

## 第一次运行

- 时间：2026-08-10 13:23（Asia/Shanghai）
- 结果：TIMEOUT
- 现象：MinerU 已成功接收任务并保持 `running`，但未在原 10 分钟等待窗口内完成；DeepSeek 尚未调用。
- 处置：保留失败截图 `../screenshots/10-real-api-analysis-failure.png`，没有把供应商排队延迟伪装成通过。

## 第二次运行

- 时间：2026-08-10 13:35（Asia/Shanghai）
- 结果：PASS
- 调整：仅为真实 E2E 把 MinerU 最大等待窗口调整为 20 分钟，业务 API 仍保持可配置超时。
- 结果：MinerU 在约 10 分钟完成；DeepSeek 随后成功生成 4 项 IP 资产、8 条逐字来源引用和 4 项发布资产。
- 凭据：API Key 仅在运行时内存读取，未写入报告或截图。

完整成功证据见 `report.md`、`analysis.json`、`publication.json` 和 `../screenshots/10-real-api-analysis.png`。
