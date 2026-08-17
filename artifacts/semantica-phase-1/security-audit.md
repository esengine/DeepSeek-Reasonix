# Semantica 第一阶段安全审查

## 结论

- Critical：0
- High：0
- Medium：0
- Low / 生产注意项：1

本阶段未发现需要阻断交付的安全问题。新增能力默认关闭，失败不会影响分析、资产库、Wiki 或搜索。

## 已验证控制

### A01 访问控制

- 只允许空间所有者和管理员调用 `/api/admin/semantic/enrich`；只读成员测试返回 403。
- 网关先调用 `listAssets(workspaceId, { role })` 完成工作空间、角色与密级过滤，再生成子进程输入。
- 子进程输出只能引用本次授权输入中的资产 ID；越权 ID 会被网关拒绝。

### A03 注入与进程隔离

- 使用 `spawn`、固定参数数组和 `shell: false`，用户输入不进入可执行文件或命令参数。
- Python 以 `-I -X utf8` 启动；运行路径要求单一绝对路径，不接受相对路径或 `PYTHONPATH` 路径列表。
- 子进程不监听端口，不接收任意脚本、代码或工具名。

### A04 资源消耗

- 单次最多 100 项资产、每项最多 20 条依据定位；输入 1 MB、输出 512 KB、超时 15 秒。
- 管理员每账号每分钟最多运行 3 次，超限返回 429 和 `Retry-After: 60`。

### A05 配置与供应链

- 固定 Semantica 0.6.0、标签对象和提交；版本不符时拒绝运行。
- 默认关闭，启用时必须显式配置绝对 Python 与源码路径。
- `npm audit --audit-level=high`：0 vulnerabilities；项目凭据扫描通过。

### 敏感数据与输出

- 不发送原始文档、证据全文、Wiki 全文、密码、会话或 API 密钥。
- 子进程只继承最小系统环境，不继承 DeepSeek/MinerU 密钥；stderr 不回传给 API。
- 前端用 `textContent` 创建结果节点，不把 Semantica 内容写入 `innerHTML`。
- 正式资产检查前后完全一致；只新增 `semantic.check` 审计事件并记录 `formalKnowledgeMutation=false`。

## Low / 生产注意项

Semantica 官方 `v0.6.0` 是未签名的 annotated tag。生产部署应从企业批准的镜像或制品库分发锁定提交，并在构建阶段校验 commit、许可证和制品哈希；不要在服务启动时在线拉取上游源码。

