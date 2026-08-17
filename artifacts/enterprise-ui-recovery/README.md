# intelifar 企业 UI 修复验收清单

验收日期：2026-08-11  
真实 UI：<http://127.0.0.1:4388/>

## 本轮修复

- 默认真实服务改为 SQLite 持久化，旧版发布资产可幂等迁移并在启动时重建全景关系图。
- 指挥台、文档中心、资产库、Wiki、审计日志和系统状态改为读取真实服务数据，持久模式不再混入静态演示指标。
- IP 全景图修复同名跨版本资产的关系解析，当前真实数据展示 8 个节点、3 条待确认关系。
- Agent 保持文档 IP 与 Wiki 的只读能力边界；本机持久空间中的任务、备份和审计记录均可追踪。
- 未知证据编号采用失败关闭策略，避免沿用上一次真实证据；脱敏演示使用独立演示证据并写入真实审计链。
- 审计导出加入表格公式注入防护；本机无认证模式仅允许回环地址，生产模式继续要求初始化管理员凭据。

## 验收结果

| 范围 | 结果 |
| --- | --- |
| Node 单元/契约测试 | 132/132 PASS |
| 前端生产构建 | PASS |
| 功能模块 UI E2E | PASS |
| 真实端口 4388 UI E2E | PASS |
| 登录、空间隔离、Wiki 版本 E2E | PASS |
| 备份、恢复与运维 E2E | PASS |
| 成员、权限、分享与审计 E2E | PASS |
| 有边界 Agent E2E | PASS |
| 真实 MinerU + DeepSeek E2E | PASS |
| 真实互联网三份材料 E2E | PASS，沉淀 26 项资产 |
| 真实 DeepSeek Agent E2E | PASS，证据覆盖率 100% |
| 凭据泄露扫描 | PASS |
| Git 补丁格式检查 | PASS |
| 原生 Windows Go 工具链预检 | PASS，Go 1.26.5 + Git Bash，无 WSL |

上游三个 Go 模块的原生 Windows 全量结果见
[Windows 原生测试报告](../windows-native-go-tests/summary.md)。本机曾由安全软件拦截 Go 临时测试程序，因此本轮未重复生成该类临时 EXE；本轮没有修改 Go 源码。

## 截图

- [真实指挥台](../real-ui-port/01-real-ui-home-4388.png)
- [IP 全景图](../real-ui-port/02-ip-panorama-4388.png)
- [有边界 Agent](../real-ui-port/03-ip-agent-4388.png)
- [脱敏演示上下文隔离](../real-ui-port/04-redaction-context-4388.png)
- [SQLite 持久化与系统状态](../real-ui-port/05-system-persistent-4388.png)

自动化明细见 [真实 UI E2E 报告](../real-ui-port/e2e-report.md)。
