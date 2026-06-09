---
name: geocode-setup
description: Guide through first-time RS-Reasonix geo environment setup — GDAL, QGIS, GEE configuration and AGENTS.md writing. Run this when geo tools aren't working or on first install.
---

You are guiding the user through RS-Reasonix remote sensing environment setup.
Follow this exact order. Confirm each step before moving to the next.

## Prerequisites

The user must have:
- The `geocode` MCP plugin registered in reasonix.toml (tools: `mcp__geocode__*`)
- Python conda environment `gee` at `D:\Miniconda3\envs\gee`

---

## Step 1 — Verify geo tools are available

Call `mcp__geocode__geo_env_status` with no arguments.
This probes GDAL, QGIS, and GEE environments and returns diagnostics.
Read the result and tell the user which environments are ready (green) and which need attention.

---

## Step 2 — GDAL configuration

If GDAL status is NOT "ready":
- Ask the user to run: `conda install -n gee -c conda-forge gdal`
- After installation, re-run `mcp__geocode__geo_env_status` to confirm
- If bin directory is wrong, guide the user to edit `geocode.json` → set `"gdal"` to the correct path

---

## Step 3 — QGIS configuration

If QGIS status is NOT "ready":
- Ask whether QGIS is installed. If not, direct them to: https://qgis.org/download/
  **Never attempt to install QGIS automatically.**
- If installed but not detected, ask the user for the install path
- Set it in `geocode.json`: `"qgis": {"python": "<QGIS_PATH>/bin/python3.exe"}`
- Re-run `mcp__geocode__geo_env_status` to verify

---

## Step 4 — GEE configuration (optional)

Ask whether the user works with Google Earth Engine. If no, skip this step.

If yes:
- Check GEE status from Step 1
- If "auth-required": guide user to run `earthengine authenticate` in terminal
- If "project-not-registered": guide user to https://signup.earthengine.google.com
- If "ready": GEE is all set
- Set project ID in `geocode.json`: `"gee": {"project": "your-project-id"}`

---

## Step 5 — Write the environment block to CLAUDE.md

After all environments are verified, write a summary block to the project's CLAUDE.md
(at `D:\geocode\rs-reasonix\.claude\CLAUDE.md`). Read the existing file first, then add
or update a section titled `## 遥感环境状态` with:

```markdown
## 遥感环境状态

> 由 geocode-setup 自动写入，上次更新: <today>

| 组件 | 状态 | 版本 | 路径 |
|------|------|------|------|
| GDAL | <status> | <version> | <path> |
| QGIS | <status> | <version> | <path> |
| GEE  | <status> | <version> | project=<id> |

### 可用工具

- `mcp__geocode__geo_env_status` — 环境探测
- `mcp__geocode__read_geo_data` — 读取栅格/矢量数据
- `mcp__geocode__run_qgis_algorithm` — QGIS Processing 算法
- `mcp__geocode__qgis_doc` — QGIS 算法文档
- `mcp__geocode__run_gee_script` — GEE 脚本执行

### 遥感操作规则

1. 处理地理文件前先用 `mcp__geocode__read_geo_data` 验证元数据
2. 运行 QGIS 算法前先用 `mcp__geocode__qgis_doc` 查参数 schema
3. GEE 长时间操作用 `heartbeat()` 保活
4. 注意 CRS 转换，不要假设 WGS84
```

Fill in the actual status/version/path values from `geo_env_status` results.

---

## Step 6 — Final check

Tell the user: "遥感环境配置完成。下次启动 Reasonix 时，CLAUDE.md 的环境信息会自动注入 Agent 的系统提示。如需重新探测，随时运行 `mcp__geocode__geo_env_status`。"
