---
description: First-time RS-Reasonix geo environment setup wizard
argument-hint: []
---

You are guiding the user through first-time RS-Reasonix setup.
Follow steps in order. Confirm each step before moving to the next.

---

## Step 1 — Find Python environment

Ask which conda environment has GDAL (e.g. gee, gaofen). Use `bash` to probe:

    conda env list

Let the user pick one. Verify GDAL exists in it:

    <conda_python> -c "from osgeo import gdal; print(gdal.__version__)"

If GDAL missing: `conda install -n <env> -c conda-forge gdal`

---

## Step 2 — Write geocode.json

Read `geocode.json` to see current state. Create or update with the paths from Step 1:

```json
{
  "gdal": "<conda_env>/Library/bin",
  "gee": {"python": "<conda_env>/python.exe", "project": ""},
  "qgis": {"python": "D:/QGIS/bin/python3.exe"}
}
```

Fill in actual paths. If QGIS path unknown, ask the user.

---

## Step 3 — Register MCP plugin

Tell the user to add this block to `reasonix.toml`:

```toml
[[plugins]]
name    = "geocode"
command = "<PYTHON_PATH>"
args    = ["-m", "internal.geo.mcp_server"]
dir     = "."
```

Replace `<PYTHON_PATH>` with the conda env python from Step 1.
Plugin takes effect after restart or `/mcp refresh`.

---

## Step 4 — Verify QGIS

Ask user if QGIS is installed. If not, direct them to https://qgis.org/download/ — NEVER auto-install.
Once confirmed, call `mcp__geocode__qgis_doc` with `action=list_groups`.
If it succeeds, QGIS is configured. If it fails, help the user fix the `qgis.python` path in `geocode.json`.

---

## Step 5 — Verify GEE (optional)

Ask if user uses Google Earth Engine. If no, skip.
If yes, call `mcp__geocode__run_gee_script` with:

    from internal.geo.mcp_server.tools.geocode import init_gee
    init_gee()
    print("GEE ok")

If it fails, guide: `earthengine authenticate`, or `conda install -n <env> -c conda-forge earthengine-api`.
Set the project ID in `geocode.json` → `gee.project`.

---

## Step 6 — Final probe

Now that everything is configured, call `mcp__geocode__geo_env_status`.
All three (GDAL/QGIS/GEE) should show "ready".
If any fail, loop back to the relevant step.

---

## Step 7 — Write global REASONIX.md

Write the environment state to the GLOBAL memory file that Reasonix loads at every session start:

  - Windows: `%APPDATA%/reasonix/REASONIX.md`
  - macOS/Linux: `~/.config/reasonix/REASONIX.md`

Use `bash` to find the exact path: `echo $APPDATA` (Windows) or `echo $HOME/.config/reasonix` (macOS/Linux).

Add or update the following sections:

### Geo Environment
Status table (GDAL/QGIS/GEE — status, version, path) from the probe.

### Geo Rules
1. Prefer mcp__geocode__* tools for all geo tasks — do not fall back to raw GDAL/bash
2. Always verify metadata with read_geo_data before processing (CRS/extent/nodata)
3. Always consult qgis_doc for parameter schema before running QGIS algorithms
4. Never assume WGS84 — explicitly handle CRS transforms per task
5. Use heartbeat() during long-running GEE operations
6. Clip/filter first, compute later — intermediate results to /vsimem/
7. Validate every generated geo file with read_geo_data after processing

### Geo Tool Registry
| Tool | Purpose |
|------|---------|
| mcp__geocode__geo_env_status | Triple-probe GDAL / QGIS / GEE |
| mcp__geocode__read_geo_data | Raster/vector metadata + preview |
| mcp__geocode__run_qgis_algorithm | QGIS Processing algorithms |
| mcp__geocode__qgis_doc | QGIS algorithm docs (422 algos) |
| mcp__geocode__run_gee_script | GEE Python scripts |

### Available Skills
These are pre-installed. Use /skill-name to invoke, or the agent auto-selects based on the task.

| Skill | Use when |
|-------|---------|
| /rsdata | Searching for remote sensing datasets |
| /gee-scripting | Writing or debugging GEE Python scripts |
| /projection-selection | Choosing a coordinate system / projection |
| /thematic-map | Creating publication-quality maps (Cartopy + frykit) |
| /multi-search-engine | Searching the web for technical information |
| /patent-architect | Drafting Chinese patent applications |
| /research-survey | Systematic technology/algorithm surveys |
| /brainstorming | Refining ideas into structured designs |

This is the same mechanism GeoCode uses (`~/.config/geocode/AGENTS.md`).

Tell the user: "Setup complete. Geo environment and skills index persisted to global REASONIX.md."
