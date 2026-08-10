# 灵遐智析交付说明

## 交付范围

本仓库来自 `esengine/DeepSeek-Reasonix` 的 `main-v2` 分支。改造保留 Reasonix 的 Go 智能代理内核，并在既有 Astro Web 栈上新增面向文档 IP 与 Wiki 的受控领域任务引擎，交付 intelifar 企业长文档 IP 智能分析与 Wiki 治理工作台；不向企业用户暴露 coding agent、Shell 或复杂文档编辑器。

官方品牌资产来自 `https://intelifar.cn/assets/`，交付内已本地化保存浅色 Logo、深色 Logo 和 favicon；主色使用官网 `#635BFF`，并扩展出企业控制台所需的墨色导航、纸张工作面与证据标记系统。

## 功能验收映射

| 技术报告验收项 | 已交付证据 | 自动化场景 |
| --- | --- | --- |
| 多格式文档分类 | 文档中心、12 种格式说明、30+ 分类、接入表单与校验 | 文档接入与表单校验 |
| IP 资产提取 | 五阶段分析流水线、业务 Schema、字段置信度、18 项结果 | 五阶段分析流水线完成 |
| IP 资产关系全景 | 权限裁剪的网络/清单双视图、关系筛选、一跳聚焦、待复核关系、图扩展搜索 | 资产全景网络筛选、聚焦与键盘访问；10k/100k 性能验收 |
| 受控 IP 任务助手 | 7 类自然语言业务任务、最多 6 步计划、只读领域工具、逐步收据、来源回链、Wiki 草案不保存、越界模型前拦截 | 真实登录 UI、影响分析、证据门禁、资产回链、越界拦截、角色限制、移动端与真实 DeepSeek |
| IP Wiki 生成 | Wiki 概览、机制、指标、关系网络、版本与来源 | Wiki 引用精准溯源 |
| 涂黑脱敏 | 不可逆涂黑预览、策略命中、版式偏移与敏感等级 | 涂黑内容权限化查看 |
| 精准溯源 | 资产字段 → 证据编号 → 文档页/内容块 → 原文高亮 | Wiki 引用精准溯源 |
| 全生命周期 | 权属、RBAC/ABAC 矩阵、安全分享、复审、归档、CSV 审计 | 生命周期安全分享；审计事件与 CSV 导出 |

## 运行与验证

要求 Node.js 22+。

```powershell
cd site
npm ci
npm test
npm run build
node .\e2e\platform.e2e.mjs
npm run test:e2e:smb
npm run test:e2e:operations
npm run test:e2e:collaboration
npm run test:e2e:modules
npm run test:e2e:agent
npm run test:e2e:agent:real
npm run test:e2e:real
npm run test:graph:performance
```

真实模式使用服务端运行时密钥。开发环境会从 `MINERU_API_KEY`、`DEEPSEEK_API_KEY` 读取；若未配置，则查找工作区上层的 `apikey.txt`。生产环境应使用密钥管理服务或环境注入，不应挂载明文文件。`npm run start:real` 默认仅监听本机的同源分析网关。

启用小微企业账号与 SQLite 持久化模式：

```powershell
$env:INTELIFAR_BOOTSTRAP_EMAIL='owner@company.com'
$env:INTELIFAR_BOOTSTRAP_PASSWORD='<从密钥管理器注入的强密码>'
$env:INTELIFAR_WORKSPACE_NAME='企业知识空间'
$env:INTELIFAR_DATABASE_PATH='C:\intelifar-data\intelifar.sqlite'
$env:INTELIFAR_BACKUP_ROOT='D:\intelifar-backups'
$env:INTELIFAR_BACKUP_RETENTION='7'
npm run start:real
```

默认 Cookie 要求 HTTPS。只在本机 HTTP 验收时可临时设置 `INTELIFAR_SECURE_COOKIES=false`；公网或反向代理部署不得关闭。`NODE_ENV=production` 下如果没有引导密码，网关会拒绝启动。

验证产物：

- `artifacts/e2e-report.md`：14 条离线浏览器 E2E 结果。
- `artifacts/screenshots/`：桌面关键流程与移动端最终截图。
- `artifacts/intelifar-audit-sample.csv`：从页面真实下载的审计样例。
- `artifacts/delivery-tree.txt`：最终交付结构清单。
- `artifacts/real-e2e/report.md`：MinerU + DeepSeek 真实调用证据。
- `artifacts/real-e2e/analysis.json`：脱敏后的真实结构化分析结果。
- `artifacts/real-e2e/mineru-preview.md`：MinerU 解析结果预览。
- `artifacts/screenshots/10-real-api-analysis.png`：真实 Provider 完成态截图。
- `artifacts/smb-p0-review/`：账号门禁、真实工作空间、Wiki V1.1 编辑、桌面与移动版本账本截图。
- `artifacts/smb-p0-report.md`：小微企业 P0 实现与验证摘要。
- `artifacts/smb-p0c-report.md`：文件隔离、可验证备份和管理员运维闭环验收摘要。
- `artifacts/smb-p0c-review/`：安全态、备份凭证、任务恢复、移动端和最终交付结构截图。
- `artifacts/smb-p0d-report.md`：成员生命周期、双凭据分享和 98.5 分复评报告。
- `artifacts/smb-p0d-review/`：邀请、激活、角色边界、公开 Wiki、撤销和最终交付结构共 10 张截图。
- `docs/INTELIFAR-USER-GUIDE.zh-CN.md`：面向外部小微企业客户的任务型中文手册，含 20 张就地截图、完整操作、故障处理和管理员检查表。
- `artifacts/user-guide-e2e-report.md`：用户手册阶段的 74 项自动化断言、全模块浏览器 E2E 和真实供应商复验报告；当前总体验收以本交付文档中的 124 项断言为准。
- `artifacts/user-guide-review/`：模块总览、证据溯源和移动端只读角色边界截图。
- `artifacts/ip-asset-graph/`：全景网络桌面/移动截图、10,000 节点与 100,000 关系性能结果及验收报告。
- `artifacts/ip-asset-graph/acceptance-report.md`：关系全景能力、测试证据、99.0 分评分和适用边界。
- `artifacts/ip-agent/`：受控任务助手桌面、来源回链、越界拦截、移动端、最终交付结构截图，以及离线和真实 DeepSeek 结果报告。
- `artifacts/real-e2e/retry-history.md`：真实 MinerU 首次排队超时及延长等待后成功的完整记录。

## 实现状态与生产边界

本次交付包含可完整操作与验证的产品级前端，以及可运行的同源分析网关。真实文件会由服务端提交 MinerU v4 完成解析，再由 DeepSeek Chat Completions JSON Output 自动完成分类、IP 提取、风险识别、Wiki 生成和逐字证据引用；浏览器只接收脱敏后的任务状态与分析结果。

企业 95+ 迭代进一步打通“真实分析 → 人工复核 → 原子发布 → 资产库 → 动态 Wiki → 证据哈希”链路。发布记录具有稳定资产/证据编号、整文档和逐字引用 SHA-256、幂等发布、重启重载和全文检索能力；前端明确披露 MinerU / DeepSeek 外部处理边界，不再使用“数据未离开企业网络”的不准确表述。最终证据见 `artifacts/enterprise-95-scorecard.md` 与 `artifacts/enterprise-95-review/`。

小微企业 P0 进一步加入 scrypt 密码、HttpOnly 会话、Owner/Admin/Editor/Viewer 四级 RBAC、工作空间级对象隔离、SQLite 事务存储、任务中断恢复、Wiki 乐观锁和只增不改版本链，以及服务端审计哈希链。SQLite 是单节点小微版适配器，不应被解释为多实例 SaaS 数据层。

小微企业 P0-C 在 MinerU 之前增加隔离扫描状态：内置确定性预检可拦截 EICAR、伪装 PE、Office 宏、高风险压缩内容与压缩炸弹指标；可按配置串联 ClamAV 等外部扫描器。恶意文件不会发送 MinerU/DeepSeek，并会删除隔离副本。SQLite 备份改用在线备份接口，生成后执行 `PRAGMA integrity_check`、表结构检查和 SHA-256 校验；owner/admin 可在“系统状态”真实 UI 中创建、复核备份并重试失败或中断任务。在线恢复因具有覆盖风险仍不开放。

小微企业 P0-D 打通自助协作闭环：owner/admin 可生成只显示一次的成员激活链接，成员自行设置强密码；角色调整和禁用均以工作空间为边界，禁用会同步撤销全部会话。安全分享采用随机链接令牌与独立访问码两个凭据，数据库只保存各自 SHA-256；公开页从 URL fragment 读取令牌后立即清除地址栏，只允许返回标题、版本、摘要、机制、指标和关系，不返回原文、逐字证据、成员或审计。分享支持到期、撤销、访问计数、水印和哈希链审计。公开脚本使用严格 CSP 允许的外部模块，并为 `.mjs`、字体与图片返回明确 MIME 类型。

资产关系全景阶段新增可重建的 SQLite 查询投影：发布快照仍是审计真相，节点、别名、关系和关系证据用于查询与展示。DeepSeek 提取的关系默认为“待复核”，人工新建关系默认为“已确认”，确认/拒绝写入审计链。Viewer 权限会在遍历前先移除机密节点，因此图、搜索、直接资产 URL 和关系 URL 都不会泄漏隐藏节点。前端提供默认全景、资产清单、类型/关系筛选、待复核虚线、关系证据卡、确认/拒绝闭环、一跳聚焦、键盘访问和移动端适配。

受控 IP 任务助手复用 Reasonix 的任务契约、规划/执行分离、步骤收据和失败预算思想，但能力被缩减为 7 类文档 IP/Wiki 意图与 7 个同源领域工具。服务端先做请求边界与角色检查，再校验 DeepSeek 计划；只允许搜索/读取当前账号可见资产、Wiki、证据和关系，以及为编辑角色准备不落库的 Wiki 草案上下文。工具执行前会重新读取当前用户状态，最终每项确定结论必须引用本次授权工具返回的资产、证据或关系编号，否则自动降级为待核实。任务按创建人和工作空间隔离，持久化计划、状态和步骤收据；不保存隐藏推理，不执行代码、命令、任意网络、外部消息、成员管理、删除、分享、发布、关系确认或 Wiki 保存。

离线演示路径仍保留，用于无外网 CI 和稳定回归；界面明确区分“离线演示”与带 `MinerU LIVE / DeepSeek LIVE` 证据的真实结果。大型企业仍需按现有基础设施接入 OIDC、托管数据库/对象存储、企业审计集群、恶意文件扫描、分片上传与分布式任务队列。对应数据边界记录在 `docs/architecture/intelifar-ip-wiki.md`。

## 小微版上线前部署责任

这些工作依赖目标域名、主机、网络、邮箱或供应商合同，不能由仓库代码替部署方做决定：

| 责任 | 上线前怎么做 | 为什么需要 |
| --- | --- | --- |
| HTTPS 与入口防护 | 由 Nginx/Caddy/云负载均衡终止 TLS，仅向内网暴露 Node 端口，保持 Secure Cookie，并配置请求体和速率限制 | 防止账号、会话和文档在传输中泄露，隔离直接主机攻击面 |
| 邮件与双通道凭据投递 | 接入企业邮箱或事务邮件服务发送一次性激活链接；安全分享的链接和访问码必须经两个不同渠道发送，并配置发信域名 SPF/DKIM/DMARC | 当前售前 MVP 只生成一次性链接，不冒充已经发信；同渠道传递两个分享凭据会削弱双凭据设计 |
| 完整恶意软件扫描 | 安装并持续更新 ClamAV，设置 `INTELIFAR_CLAMSCAN_PATH` 与 `INTELIFAR_REQUIRE_EXTERNAL_AV=true`，用 EICAR 做上线验收 | 内置预检是高置信规则层，不是完整病毒特征库；故障关闭可避免扫描失效时放行 |
| 备份调度与异地主副本 | 用计划任务定期调用管理员备份流程或封装服务方法，将生成的 SQLite + manifest 加密复制到另一故障域，并每月至少恢复演练一次 | 同机备份无法抵御磁盘损坏、勒索软件或主机丢失；只有恢复演练能证明备份可用 |
| 离线恢复流程 | 停止网关，保留当前库，复核 manifest 哈希及完整性，把选定快照恢复到新路径，修改 `INTELIFAR_DATABASE_PATH` 后启动并做登录、Wiki、审计烟测 | 在线覆盖可能造成写入竞争和不可逆数据损坏，因此本版本刻意不提供 Web 恢复按钮 |
| 主机与磁盘安全 | 开启磁盘加密、最小权限服务账号、系统补丁、端点防护和数据库/备份目录 ACL | SQLite、隔离文件和备份都可能包含客户知识资产，主机失陷会绕过应用 RBAC |
| 监控与告警 | 采集进程、磁盘空间、HTTP 5xx、分析失败率、扫描器不可用和备份过期指标，设置责任人和通知渠道 | 小微版没有 7×24 平台团队，必须把静默失败变成可行动告警 |
| 供应商与数据合规 | 与 MinerU、DeepSeek 确认数据用途、留存、地域和删除条款，在客户合同及隐私说明中披露数据边界 | 真实链路会把原文发送 MinerU、受限解析文本发送 DeepSeek，技术加密不能替代合同和告知义务 |
| Agent 配额与中断恢复 | 为 DeepSeek 任务配置用户/空间配额、成本告警和超时；进程重启后由管理员查看“已中断”任务并让原创建人重试 | 当前小微版在单 Node 进程内执行任务并持久化收据，不是分布式队列；外部模型延迟或重启不应被误报为已完成 |

恢复演练完成前，页面中的“备份完整性通过”只证明快照文件可读且哈希一致，不代表组织已经具备灾难恢复能力。

## 当前评分与下一产品阶段

按“售前 MVP / 单实例小微企业”口径当前为 **99.0/100**：核心 IP 分析链路 20/20、Wiki、证据与关系治理 20/20、安全、租户隔离及协作 20/20、可靠性及运维 19.5/20、售前体验 19.5/20。扣分来自尚未内置的备份过期主动告警，以及事务邮件投递、自助密码重置与可选 TOTP MFA；这些缺口均已真实标注，没有由静态演示替代。

99.0 分由 124 项自动化断言、14 条离线浏览器场景、认证 Wiki E2E、安全运维 E2E、协作分享 E2E、9 模块与角色边界 E2E、受控任务助手 E2E、10,000 节点/100,000 关系性能基准、真实 MinerU → DeepSeek E2E、真实 DeepSeek Agent E2E、0 依赖漏洞和 0 运行时密钥泄漏共同支撑。该分数不适用于大型企业生产部署；大型企业仍需结合现有 IdP、数据库、对象存储、队列和 SIEM 重新验收。

继续产品化时，优先级依次为事务邮件投递与退信处理、自助密码重置/可选 TOTP MFA，以及备份过期主动告警。跨 Wiki 关系已经具备图查询、搜索扩展和逐条复核能力；后续批量编辑属于效率增强项。它们会提升自助运营效率，但不改变当前小微售前 MVP 已超过 98 分的结论。

## 上游许可

Reasonix 原项目使用 MIT License，仓库中的 `LICENSE`、原始文档和归属信息均保留。intelifar Logo 与品牌资产仅用于本次指定品牌改造。
