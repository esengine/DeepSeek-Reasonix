# Reasonix project memory

This file is loaded into every session's system prompt (the cache-stable prefix),
so keep it concise and durable — it is the project's standing instructions to the
agent. It is the Reasonix analog of Claude Code's CLAUDE.md.

## Conventions

- Go kernel under `internal/`; each package owns one concern and documents it in a
  package comment. Match the surrounding comment density and idiom when editing.
- One transport-agnostic `control.Controller` sits behind every frontend (chat
  TUI, HTTP/SSE serve, Wails desktop). Add behavior to the controller, not a
  frontend, so all three inherit it.
- Cache-first: the system-prompt prefix (base prompt + tools + memory) must stay
  byte-stable across turns so DeepSeek's automatic prefix cache stays warm. Never
  mutate it mid-session — ride the turn tail instead (see `control.Compose`).

## Memory

- Hierarchical docs: `REASONIX.md` (this file, committed/shared), `REASONIX.local.md`
  (personal, git-ignored), user-global `~/.config/reasonix/REASONIX.md`, and any
  `REASONIX.md` in an ancestor dir. `AGENTS.md` is accepted as a fallback name.
- `@path` on its own line imports another file's contents.
- `#<note>` in chat quick-adds a line here. The `remember` tool saves durable
  facts to the per-project auto-memory store (frontmatter files + `MEMORY.md`
  index), which loads into the prefix on the next session.

## Geo Environment

> Auto-probed. Re-probe: `mcp__geocode__geo_env_status`.

| Component | Status | Version | Path |
|-----------|--------|---------|------|
| GDAL | ready | 3.13.0 | D:\anaconda\anaconda3\envs\gee\Library\bin |
| QGIS | ready | — | D:\QGIS\bin\python3.exe |
| GEE  | ready | 1.7.26 | project=ee-copythree666 |

### Geo Rules

1. Prefer `mcp__geocode__*` tools for all geo tasks — do not fall back to raw GDAL/bash
2. Always verify metadata with `mcp__geocode__read_geo_data` before processing (CRS/extent/nodata)
3. Always consult `mcp__geocode__qgis_doc` for parameter schema before running QGIS algorithms
4. Never assume WGS84 — explicitly handle CRS transforms per task
5. Use `heartbeat()` during long-running GEE operations to prevent idle timeout
6. Clip/filter first, compute later — intermediate results go to `/vsimem/`
7. Validate every generated geo file with `read_geo_data` after processing

### Geo Tool Registry

| Tool | Purpose |
|------|---------|
| `mcp__geocode__geo_env_status` | Triple-probe GDAL / QGIS / GEE environments |
| `mcp__geocode__read_geo_data` | Raster/vector metadata + WebP/GeoJSON preview |
| `mcp__geocode__run_qgis_algorithm` | Execute QGIS Processing algorithms |
| `mcp__geocode__qgis_doc` | Browse QGIS algorithm documentation (422 algos) |
| `mcp__geocode__run_gee_script` | Execute GEE Python scripts + geocode helpers |

## Notes
