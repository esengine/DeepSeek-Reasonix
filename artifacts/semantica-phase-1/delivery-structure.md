# Semantica 第一阶段交付结构

```text
intelifar-ip-wiki-graph/
├─ integrations/semantica/
│  ├─ bridge.py                         # 隔离 Python JSON 桥接器
│  ├─ test_bridge.py                    # 官方 v0.6.0 模块真实集成测试
│  ├─ semantica.lock.json               # 版本、标签对象、提交和许可证锁
│  └─ README.md                         # 部署与边界说明
├─ site/server/
│  ├─ semantica-client.mjs              # Node 受控进程适配器
│  ├─ semantica-client.test.mjs         # 输入、输出、版本和失败降级测试
│  └─ real-analysis-server.mjs          # 管理员 API、权限、限流与审计
├─ site/e2e/semantica-real.e2e.mjs      # 4388 真实资产无写入 E2E
├─ site/src/
│  ├─ pages/index.astro                 # 系统页“语义资产体检”
│  ├─ scripts/ip-platform.mjs           # 状态、触发和结果呈现
│  └─ styles/ip-platform.css            # 响应式业务界面
├─ docs/architecture/adr/
│  └─ 0003-use-optional-semantica-sidecar.md
├─ docs/plans/
│  ├─ 2026-08-11-semantica-phase-1-design.md
│  └─ 2026-08-11-semantica-phase-1.md
└─ artifacts/semantica-phase-1/
   ├─ 01-ready.png                      # 真实 UI 可用状态
   ├─ 02-result.png                     # 真实系统页完整结果
   ├─ 03-result-panel.png               # 结果面板近景
   ├─ result.json                       # 真实资产检查结果
   ├─ report.md                         # 无写入 E2E 报告
   ├─ security-audit.md                 # OWASP 视角安全审查
   └─ delivery-structure.md             # 本文件
```

