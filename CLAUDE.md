# RS-Reasonix — 遥感定制版 AI Coding Agent

Fork 自 [esengine/deepseek-reasonix](https://github.com/esengine/deepseek-reasonix)，融合 GeoCode 遥感/GIS 能力。

## 项目结构

```
RS-Reasonix/
├── cmd/reasonix/              # CLI 入口
├── internal/
│   ├── agent/                 # Agent 循环、会话、协调
│   ├── cli/                   # TUI、子命令
│   ├── config/                # TOML 配置
│   ├── plugin/                # MCP 插件框架（Go 客户端）
│   ├── tool/builtin/          # 内置工具（bash、read_file 等）
│   └── geo/                   # ← GeoCode 遥感模块（本分支新增）
│       └── mcp_server/        # Python MCP Server
│           ├── server.py      # JSON-RPC 2.0 协议层
│           ├── geo_tools.py   # 5 个工具注册
│           ├── http_server.py # 预览 HTTP 服务（随机端口）
│           └── tools/         # 工具实现
├── desktop/                   # Wails 桌面端（Go + React）
└── .reasonix/                 # Reasonix 配置和 Skills
```

## 当前分支

`feat/geo-mcp-plugin` — GeoCode 功能迁移（开发中）。
`main-v2` 追踪官方 upstream，不在上面直接改代码。

## 遥感环境

| 组件 | 状态 | 路径/版本 |
|------|------|-----------|
| Conda 环境 | `gee` | `D:\Miniconda3\envs\gee` (Python 3.10) |
| GDAL | ready (3.13.0) | conda gee env 的 `Library/bin` |
| QGIS | ready (3.44.10) | `C:\Program Files\QGIS 3.44.10` |
| GEE | 已安装 (1.7.26) | 认证状态待确认 |

## Python MCP Server

所有遥感工具通过 Python MCP Server 暴露，Reasonix Go 端通过 stdio JSON-RPC 2.0 调用。

### 启动命令

```bash
conda run -n gee python -m internal.geo.mcp_server
```

### 5 个工具

| 工具 | 状态 | 说明 |
|------|------|------|
| `geo_env_status` | 完成 | GDAL/QGIS/GEE 三重环境探测 |
| `read_geo_data` | 完成 | 栅格/矢量元数据 + WebP/GeoJSON 预览 |
| `run_qgis_algorithm` | stub | QGIS Processing 算法调用 |
| `qgis_doc` | stub | QGIS 算法文档搜索 |
| `run_gee_script` | stub | GEE Python 脚本执行 |

### 冒烟测试

```bash
cd RS-Reasonix
conda run -n gee python internal/geo/mcp_server/test_mcp.py
```

### 注册到 Reasonix

```toml
# reasonix.toml
[[plugins]]
name    = "geocode"
command = "D:\\Miniconda3\\envs\\gee\\python.exe"
args    = ["-m", "internal.geo.mcp_server"]
```

## QGIS 调用要点

QGIS standalone 的 Python 不能直接运行，必须通过 `python-qgis-ltr.bat` 桥接：

```python
# 正确方式：bat-bridge
subprocess.run(
    f'"{qgis_root}\\bin\\python-qgis-ltr.bat" "{script_path}"',
    shell=True, env={"PYTHONHOME": f"{qgis_root}\\apps\\Python312"}
)

# 错误方式：直接调 python3.exe（会报 DLL 冲突 STATUS_STACK_BUFFER_OVERRUN）
```

probe 脚本中 `processing` 模块导入必须在 `qgs.initQgis()` 之后，且需手动将 `plugins` 加入 `sys.path`。

## Git 工作流

```
upstream (esengine/deepseek-reasonix)  → 只读
origin   (liuyuan1041/RS-Reasonix)     → 可 push
本地      feat/geo-mcp-plugin           → 开发分支
```

每次开发前同步上游：
```bash
git fetch upstream
git checkout main-v2 && git merge upstream/main-v2 && git push origin main-v2
git checkout feat/geo-mcp-plugin && git merge main-v2
```

每完成一个 step 后提交并推送。

## 关键路径

| 资源 | 路径 |
|------|------|
| 迁移方案 | `D:\geocode\RS-Reasonix-GeoCode-Migration-Plan.md` |
| 改动记录 | `D:\geocode\gaidong\` |
| GeoCode 源码（参考） | `D:\geocode\GeoCode-source-v0.9.2\` |
| QGIS 安装 | `C:\Program Files\QGIS 3.44.10` |
| 项目 .claude/ | `D:\geocode\rs-reasonix\.claude\` |

## 禁止行为

- 不在 `main-v2` 上直接改代码
- 不碰 `internal/plugin/plugin.go`（Go 核心协议层）
- 不改 Reasonix 已有 UI 组件（除非第二批迁移）
- MCP Server 的 stdout 严禁输出调试信息（JSON-RPC 协议要求）
