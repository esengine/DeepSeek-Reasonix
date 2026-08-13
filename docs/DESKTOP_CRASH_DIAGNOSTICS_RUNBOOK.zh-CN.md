# Windows / Linux Desktop 崩溃诊断运行手册

<a href="./DESKTOP_CRASH_DIAGNOSTICS_RUNBOOK.md">English</a>

本手册用于跨平台 Desktop 诊断链路的发布、隐私、性能和根因闭环。Windows build
`17763` 是重点实验环境，不是代码白名单；发布诊断版本本身不代表问题已经解决。

## 发布顺序

1. 冻结唯一候选 SHA，已发布 tag 不得移动或重建。
2. 备份 D1，并先用 `PRAGMA table_info` 检查生产，再应用
   `workers/crash-report/migrate-diagnostics-v2.sql`。若 draft 字段已提前存在，停止发布，
   另做纯加法 reconciliation migration。
3. 验证 `report_daily`、`report_installations`、
   `report_event_dimensions`、`diagnostics_meta`、fingerprint/date 索引、ping
   窗口索引，以及 `installation_linked_since`。
4. 先部署 Worker；用旧 Report/Ping/Metrics、legacy `webview2`、Windows/Linux
   `webRuntime` payload 做 `channel=test` smoke。
5. 用同一 SHA 生成签名 Windows/Linux 构建；能力矩阵和性能门禁通过后才发布 feature
   release。
6. 通过管理界面保留审计地整理历史数据：忽略 `[go panic] safe` / `v9.9.9`，将
   `72daba81` 标记为在 `desktop-v1.19.3` 解决，忽略旧
   `desktop.abnormal_exit` replay 分组。

## 隐私与兼容 smoke

旧 payload 可以缺失所有新增字段；legacy `webview2` 必须归一化为 `webRuntime`。
同一 engine/kind/reason/exit code 的恢复成功与失败必须属于同一 fingerprint。还需验证：

- 原始 install ID 不进入样本、HTML、应用/审计日志、导出和 pending 文件；
- 模块只保留 basename；不包含内容、密钥、账号、hostname、完整路径、GPU 型号和驱动；
- 重复事件正确累加 daily/install/event-dimension，且早期环境组合不被覆盖；
- 删除测试分组会删除三张诊断聚合表的对应数据；
- 诊断事实、ping、metric user 按 30 天分块清理；
- `channel=test` 始终位于 development namespace。

## 正常体验门禁

候选版本在 Wails 启动前只允许一次本地配置读取、一次非阻塞归属锁和一次小型原子生命周期
写入。Runtime 探测及报告/指标落盘必须在 Wails 启动后或有界后台消费者中执行；COM/GTK
回调只能非阻塞入队或递增原子丢弃计数。任何诊断失败都必须 fail-open。

使用同一 SHA 与关闭诊断的基线比较：诊断初始化 p95 不超过 10 ms、p99 不超过 25
ms；DOM-ready p95 回归不超过 `max(20 ms, 2%)`；shutdown p95 回归不超过 20 ms；
空闲 CPU 增幅小于 0.1 个百分点；RSS 增幅不超过 2 MiB。30 分钟正常使用期间必须是：
0 次诊断 reload、0 个轮询 timer、0 个新增弹窗，除已有 ping/metrics 外 0 个额外请求。

## 能力认证矩阵

全过程使用同一候选 SHA。Runtime、GPU 与驱动信息只记录在私密实验表，客户端不采集驱动。

| 平台 | 必须覆盖 |
| --- | --- |
| Windows 10 LTSC 2019 `17763` | VM + 实体 GPU；系统及最新 Evergreen WebView2；GPU 开/关 |
| Windows 10 `19045` | x64 对照；系统及 Evergreen WebView2 |
| Windows 11 稳定版 | x64 实机及当前稳定 Runtime |
| Windows arm64 | 正式交叉构建 + 一台设备 smoke |
| Ubuntu 22.04 | WebKitGTK 4.0、X11 |
| Ubuntu 24.04 | WebKitGTK 4.1、X11、Wayland |
| Debian 12、Fedora stable、Arch rolling | 能力 smoke；本地会话；Intel/AMD/NVIDIA 代表性覆盖 |
| 远程会话 | Windows RDP 与 Linux remote/xrdp |

每个环境执行 20 次冷启动/正常退出、10 次更新重启、60 分钟工作负载、50 次最小化/
恢复，以及休眠、显示器/DPI、远程连接切换。测试构建可定向终止 renderer/web process，
验证只恢复一次。Windows 收集 WER/可靠性监视器，Linux 收集 journal/coredump 元数据。
dump/core 仅在用户明确授权后私密传输，并在分析后删除。

## 根因和观察闭环

环境关联至少满足一项：两台同类实验节点复现且对照不复现；或三个不同线上安装命中同一
fingerprint，同时该环境至少有 30 个活跃安装，影响率达到对照 3 倍。GPU workaround
要求每台 GPU-on 至少 `2/20`、两台 GPU-off 合计 `0/40`，且每台两小时长测为 0。
workaround 必须按已有能力/Runtime 证据限定，不能只按发行版名称或 Windows build。

Integrity failure 转签名、注入和安全软件调查；OOM 转内存与会话资源调查；Runtime
聚集才支持后续最低版本或升级策略。renderer 恢复成功不算应用崩溃；只有 lifecycle
abnormal exit 时，必须拿到 WER、journal、dump 或 core 之一才能结案。

上线后观察七个完整 UTC 日：身份覆盖率目标 95%；低于 90% 不展示精确影响率。每天检查
legacy replay、fatal/recovered/degraded 数量关系、recovery failure、平台/Runtime/GPU
影响率、D1 增长、retention 和查询耗时。证据不足就保持 open 并延长至 30 天。只有根因
被证实后才发布定向补丁，修复后实验室要求 `0/40` 复现。
