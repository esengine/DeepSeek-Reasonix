---
description: Probe and configure geo remote-sensing environment (GDAL / QGIS / GEE)
---

Call `mcp__geocode__geo_env_status`. Read the result. Report which environments are ready (green) and which need attention.

Then read `RS-Reasonix/geocode.json`. Cross-check each path against the probe result. If any path is wrong or points to a non-existent location, flag it.

After verifying, inject or update a `## Geo Environment` section into `RS-Reasonix/REASONIX.md` containing the environment status table, geo operating rules, and tool registry. Use the exact values from the probe.

Tell the user: "Geo environment configured. State persisted to REASONIX.md. Re-probe any time with `/set` or `mcp__geocode__geo_env_status`."
