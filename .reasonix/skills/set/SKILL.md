---
name: set
description: Probe and configure RS-Reasonix geo remote-sensing environment — GDAL, QGIS, GEE detection, geocode.json update, REASONIX.md persistence. Run this on first install or when geo tools aren't working.
---

You are guiding the user through RS-Reasonix remote sensing environment setup.
Follow this flow: **probe → compare → fix → inject → verify**.

REASONIX.md is the project memory file loaded into the system prompt prefix at boot.
The geo environment block written there becomes a persistent set of agent operating instructions.

## Step 1 — Probe

Call `mcp__geocode__geo_env_status` with no arguments.
This probes GDAL (gdalinfo/ogrinfo/bin tools), QGIS (subprocess via python-qgis-ltr.bat),
and GEE (ee.Initialize with auth check).

Extract the `__env_block__` JSON from the response for structured values.

## Step 2 — Compare against geocode.json

Read `RS-Reasonix/geocode.json`. Cross-check:
- Does `gdal` match the probe's bin_dir?
- Does `qgis.python` exist on disk?
- Does `gee.python` exist on disk?
- Any paths pointing to old `Miniconda3` or old QGIS install locations?

Flag mismatches and fix them.

## Step 3 — Fix missing components

For each component NOT "ready":
- **GDAL**: `conda install -n gee -c conda-forge gdal`
- **QGIS**: verify install at `D:\QGIS`, check `bin\python-qgis-ltr.bat` exists. Download from https://qgis.org if missing.
  Never attempt to install QGIS automatically.
- **GEE**: `earthengine authenticate` in terminal, or register project at https://signup.earthengine.google.com

Update `geocode.json` with corrected paths. Re-run `mcp__geocode__geo_env_status` to confirm.

## Step 4 — Inject into REASONIX.md

Read the current `RS-Reasonix/REASONIX.md`. Preserve ALL existing content.
Insert a `## Geo Environment` section BEFORE `## Notes`. Fill with:

```markdown
## Geo Environment

> Auto-probed by `/set` on <today>. Re-probe: `mcp__geocode__geo_env_status`.

| Component | Status | Version | Path |
|-----------|--------|---------|------|
| GDAL | <status> | <version> | <bin_dir> |
| QGIS | <status> | <version> | <python_path> |
| GEE  | <status> | <version> | project=<id> |

### Geo Rules

1. 遥感任务优先 mcp__geocode__* 工具，不回退到手写 GDAL/bash
2. 处理地理文件前必用 `mcp__geocode__read_geo_data` 验证元数据 (CRS/范围/nodata)
3. 运行 QGIS 算法前必用 `mcp__geocode__qgis_doc` 查参数 schema
4. 坐标系不能假设 WGS84，显式处理 CRS 转换
5. GEE 长时间操作用 heartbeat() 保活
6. 多步骤工作流先裁剪再计算，中间结果走 /vsimem/
7. 生成的每个地理文件用 read_geo_data 验证输出

### Geo Tool Registry

| Tool | Purpose |
|------|---------|
| `mcp__geocode__geo_env_status` | 环境探测 |
| `mcp__geocode__read_geo_data` | 栅格/矢量元数据 + 预览 |
| `mcp__geocode__run_qgis_algorithm` | QGIS Processing 算法 |
| `mcp__geocode__qgis_doc` | QGIS 算法文档 |
| `mcp__geocode__run_gee_script` | GEE Python 脚本 |
```

Fill in actual values from Step 1 probe results.

## Step 5 — Verify

Call `mcp__geocode__geo_env_status` one final time. All three components should be "ready".

Tell the user: "Geo environment configured. State persisted to REASONIX.md —
Reasonix loads this as project memory at boot. Re-probe anytime with
`mcp__geocode__geo_env_status` or re-run `/set`."
