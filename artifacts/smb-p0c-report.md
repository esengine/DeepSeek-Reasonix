# intelifar 小微企业 P0-C 安全运维验收报告

- 验收日期：2026-08-10
- 结果：PASS
- 评分口径：售前 MVP / 单实例小微企业
- 当前评分：96/100

## 本阶段交付

- 上传隔离：文件进入 MinerU 前先完成安全决策，隔离目录使用工作空间 SHA-256 派生段，避免路径穿越。
- 内置预检：EICAR、伪装 PE、Office 宏、归档内活动内容、畸形 ZIP、条目上限和压缩炸弹指标。
- 外部扫描适配器：支持部署方配置 ClamAV；`INTELIFAR_REQUIRE_EXTERNAL_AV=true` 时扫描器不可用即故障关闭。
- 安全状态：`security-scan`、`blocked` 和扫描元数据进入持久任务；恶意文件删除隔离副本且不可重试，扫描器临时不可用则可重试。
- 在线备份：better-sqlite3 在线备份、只读 `PRAGMA integrity_check`、所需表结构校验、SHA-256 manifest、原子发布和默认 7 份保留。
- 管理权限：owner/admin 可查看运维态、创建及验证备份；viewer API 实测 403；不开放破坏性的在线恢复。
- 任务恢复：管理员 UI 可重试失败或服务重启中断任务，重试会重新执行文件安全检查。
- 真实 UI：intelifar 系统状态页新增扫描、运行库、审计链、备份凭证与恢复队列，完成桌面和 390×844 移动视觉复核。

## 自动化与真实服务证据

- Node 单元/契约/API：59 个顶层测试、64 个含嵌套测试，全部通过。
- 离线浏览器：12 个 E2E 场景通过。
- SMB 认证 Wiki：登录、工作空间隔离、真实分析、发布、V1.1 编辑和版本历史通过。
- 安全运维 E2E：EICAR 拦截且 MinerU/DeepSeek 调用计数均为 0；在线备份、再次校验、中断任务恢复和移动端通过。
- 真实供应商 E2E：MinerU → DeepSeek `deepseek-v4-flash` → 发布通过，用时 20.9 秒，2,929 tokens，4 项资产、8 条来源引用。
- Astro 生产构建：48 个静态页面构建通过。
- 依赖审计：`npm audit --omit=dev` 为 0 个已知漏洞。
- 凭据扫描：通过；运行时 API Key 未写入项目文本产物。
- 补丁质量：`git diff --check` 通过。

## 截图证据

1. `01-operations-posture.png`：外部扫描配置、运行库、审计链、EICAR 拦截和中断恢复入口。
2. `02-verified-backup-ledger.png`：备份编号、时间、大小、SHA-256 和复核操作。
3. `03-recovery-complete.png`：中断任务重新经过安全检查并完成 MinerU + DeepSeek。
4. `04-mobile-operations.png`：390×844 移动端完整运维台。
5. `05-final-delivery-structure.png`：最终交付物结构留档。

## 未冒充完成的边界

开发机未安装真实 ClamAV，自动化中使用可插拔测试扫描器验证外部扫描协议；内置确定性预检已真实执行，但生产上线必须安装并更新外部特征库。仓库内备份证明 SQLite 快照完整，不等同于异地容灾；部署方仍需调度、加密复制和恢复演练。成员邀请、密码重置/MFA、服务端安全分享与备份过期告警进入下一产品阶段。
