---
description: First-time RS-Reasonix geo environment setup wizard
argument-hint: []
---

You are guiding the user through RS-Reasonix geo remote-sensing environment setup.
The user just invoked `/set`. Follow this exact order. Confirm each step before moving to the next.

---

## Step 1 — Verify Python + GDAL environment

Ask the user which conda environment has GDAL installed (e.g., `gee`, `gaofen`).
If they're unsure, suggest running: `conda env list`

Once confirmed, call `mcp__geocode__geo_env_status`. Read the GDAL section.
If GDAL status is NOT "ready", guide them to install GDAL:

    conda install -n <env> -c conda-forge gdal

Re-run `geo_env_status` after installation to confirm.

---

## Step 2 — Write the geocode.json config

Read `RS-Reasonix/geocode.json`. If it doesn't exist or has wrong paths, update it:

```json
{
  "gdal": "<conda_env>/Library/bin",
  "gee": {"python": "<conda_env>/python.exe", "project": "ee-copythree666"},
  "qgis": {"python": "D:/QGIS/bin/python3.exe"}
}
```

Fill in the actual paths from Step 1. Ask the user for their QGIS install path if needed.

---

## Step 3 — Register the MCP plugin

Tell the user to edit `reasonix.toml` and add the geocode plugin block.
Show them the exact text to paste (with their paths filled in):

```toml
[[plugins]]
name    = "geocode"
command = "<PYTHON_PATH>"
args    = ["-m", "internal.geo.mcp_server"]
dir     = "."   # RS-Reasonix project root
```

---

## Step 4 — Verify QGIS

Call `mcp__geocode__geo_env_status` again. Check the QGIS section.
If QGIS status is NOT "ready":
- Ask the user where QGIS is installed
- If not installed, direct them to https://qgis.org/download/ (NEVER auto-install)
- Update `geocode.json` → `qgis.python` to point to `QGIS/bin/python3.exe`

---

## Step 5 — Verify GEE (optional)

Ask whether the user works with Google Earth Engine. If no, skip.
If yes, check GEE status from `geo_env_status`.
- "auth-required": guide to run `earthengine authenticate`
- "not-installed": guide to `conda install -n <env> -c conda-forge earthengine-api`
- Update `geocode.json` → `gee.project` with their GCP project ID

---

## Step 6 — Write the environment block to REASONIX.md

After all environments are verified, read `RS-Reasonix/REASONIX.md`.
Add or update a `## Geo Environment` section with:

- Environment status table (GDAL/QGIS/GEE — status, version, path)
- 7 Geo Rules (tool priority, CRS handling, heartbeat, etc.)
- Geo Tool Registry table (5 MCP tools with descriptions)

Use the actual values from the `geo_env_status` probe.

---

## Step 7 — Final check

Tell the user: "RS-Reasonix geo environment configured.
- Environment state persisted to REASONIX.md (loaded at every session start)
- Re-probe any time with `mcp__geocode__geo_env_status`
- Re-run `/set` to update configuration"
