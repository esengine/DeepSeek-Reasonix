# intelifar 小微企业 P0 验收报告

- 验收日期：2026-08-10
- 结果：PASS
- 产品形态：单节点小微企业适配器
- 身份：scrypt 密码、HttpOnly/SameSite 会话、Owner/Admin/Editor/Viewer RBAC
- 隔离：分析、资产、Wiki、证据与搜索按 `workspace_id` 服务端过滤
- 存储：SQLite WAL 事务、外键、唯一约束、幂等发布
- 任务：状态持久化、重启中断标记、保留上传的显式重试
- Wiki：人工编辑、乐观锁、V1.0 → V1.1 只增不改版本链
- 审计：服务端事件、按工作空间 SHA-256 哈希链

## 自动化证据

- 单元/契约/API：47 项通过
- 离线浏览器：12 项通过
- SMB 认证浏览器：登录 → 工作空间 → 分析 → 发布 → 编辑 V1.1 → 历史版本通过
- 真实供应商：MinerU → DeepSeek `deepseek-v4-flash` → 发布通过，耗时约 20 秒
- 依赖审计：0 个已知漏洞（安装时 npm audit）
- 凭据扫描：通过
- Astro 生产构建：通过

## 截图

1. `01-smb-secure-login.png`：安全登录门禁
2. `02-workspace-identity.png`：真实工作空间与角色
3. `03-wiki-version-edit.png`：Wiki 人工复核编辑
4. `04-wiki-reviewed-v11.png`：V1.1 发布结果
5. `05-wiki-version-ledger.png`：桌面版本账本
6. `06-mobile-version-ledger.png`：390×844 移动版本账本

## 仍未冒充完成的边界

SQLite 适用于单实例售前和小微版，不提供多节点写入、对象存储、托管 PITR 或跨区域容灾。公网接收真实客户文件前仍须由部署方配置 HTTPS、磁盘加密、自动备份、恶意文件扫描、供应商数据协议和主机监控。大型企业继续按其现有 IdP、数据库、对象存储、队列与 SIEM 接入。
