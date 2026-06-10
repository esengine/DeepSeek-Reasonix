---
name: GEEmu
description: Use when Codex needs to answer questions or write Python code for Google Earth Engine (GEE), the Earth Engine Python API, geemap, ee.Image/ImageCollection/FeatureCollection workflows, remote-sensing scripts, exports, reducers, cloud masking, dataset lookup, or Qiongwei's local GEE/geemap knowledge and practical experience.
---

# GEEMu Skill

Use this skill to write practical Google Earth Engine and geemap workflows from
the bundled GEEMu references and local vector knowledge base.

## Start Here: Project ID First, Then Verify

The first action in every GEEMu task — before retrieval, research design, or any
code — is to establish the Cloud Project ID and verify the environment, in order:

1. **Ask the user for their Earth Engine Cloud Project ID** if it was not already
   given. Do not guess it and do not continue without it; every online Earth
   Engine call needs `Initialize(project=...)`. (Tip: the user can set
   `EE_PROJECT` once to avoid re-entering it each session.)
2. **Verify the environment** with that Project ID: confirm `ee` and `geemap`
   import, Earth Engine credentials exist, and
   `geemap.ee_initialize(project="<PROJECT_ID>")` (or
   `ee.Initialize(project="<PROJECT_ID>")`) succeeds. The deterministic check is
   `python scripts/check_local_environment.py --project <PROJECT_ID>`.
3. Only after the Project ID is confirmed and initialization succeeds, continue
   to retrieval, research design, and code.

If verification fails, diagnose in this order: (1) **restricted network — check
the `HTTP_PROXY` / `HTTPS_PROXY` environment variables first**, since blocked
Google connectivity is the most common cause (see `references/network_proxy.md`);
(2) missing credentials → run `earthengine authenticate`; (3) other errors → see
`references/local_environment.md`. Resolve this before writing task code.

## Included Knowledge

- `gee_vector_db/`: portable local knowledge database derived from the full
  GEEMu bundle and practical experience notes.
- `awesome-gee-community-datasets/community_datasets.csv`: structured community
  dataset index.
- `references/`: GEEMu workflow gates for environment, proxy, research design,
  data semantics, boundaries, local vectors, and compute strategy.
- `templates/`: run-log and data-layer templates.
- `examples/`: compact runnable examples.

This compact full GitHub package keeps the complete usable GEEMu workflow through
the bundled local knowledge DB, references, examples, and community CSV. It does
not carry the redundant raw `google-earth-engine/` or `geemap/` documentation
snapshots. Search is keyword-based and does not require an embedding model
download. Prefer the bundled local knowledge DB first; use source URLs or
official online docs when a direct API detail must be verified.

## Retrieval Workflow

For non-trivial questions, search local knowledge before answering or coding:

```powershell
python _gee_vector_db.py search "<query>" --db gee_vector_db --limit 8
```

For community dataset lookup:

```powershell
rg -n "<term>" awesome-gee-community-datasets/community_datasets.csv
```

Prefer evidence in this order:

1. Official Earth Engine behavior found in vector hits or source URLs.
2. geemap usage patterns found in vector hits or examples.
3. Qiongwei's local practical experience represented in the knowledge DB.
4. Community dataset catalog rows, after checking license and docs links.

## Required Gates

Before writing final code, run the relevant gates:

- Startup gate: open `references/local_environment.md`; confirm Python can import
  `ee` and `geemap`, Earth Engine credentials exist, a Cloud Project ID is known,
  and online initialization succeeds. If the Project ID is missing, stop and ask.
- Network/proxy: for restricted networks, open `references/network_proxy.md`.
- Research design: for open-ended tasks, open `references/research_design.md`;
  decide study area, analysis scale/resolution, and output target. Use local
  knowledge to suggest options when the user has not specified them.
- Local vector ROI: for shapefile, GeoJSON, GeoPackage, or GeoDataFrame inputs,
  open `references/local_vector_roi.md`.
- Data semantics: for dataset choice, bands, units, scale/offset, QA masks, or
  transformations, open `references/data_layer.md`.
- Boundary and compute: for ROI complexity, bbox, clipping, tiling, exports,
  memory, or EECU risk, open `references/boundary_compute.md`.
- Traceability: save generated scripts and Markdown logs under
  `runs/YYYYMMDD-HHMM-task-name/`.

Ask clarifying questions when missing information changes correctness, cost,
network access, or data meaning. If a missing value is routine, use an explicit
placeholder and record the assumption.

## Confirm Before Running

Before running a task or executing final code against Earth Engine, restate the
key choices and ask the user to confirm or adjust them. Earth Engine runs cost
compute, and resolution / region / output choices are easy to get wrong, so a
short confirmation prevents wasted runs, surprise costs, and files landing in the
wrong place.

Confirm at least:

- Output target: local GeoTIFF download (where on disk?), Google Drive export,
  Earth Engine Asset, map preview only, or a numeric/CSV summary. Each is a
  different code path and cost profile, so always confirm which one.
- Analysis scale / resolution: the compute and export scale in meters, and
  whether it is the dataset's native resolution or a coarser test scale.
- Study area / ROI and time range.
- Dataset and key transforms (bands, masks, thresholds) when not already fixed.
- Cost / size risk: region extent, pixel count, number of tiles or export tasks.

Present this as a short, concrete summary (a few lines or a small table) with
sensible defaults drawn from local knowledge, so the user can confirm in one
step. Do not re-ask values the user already gave. Use the research-design gate to
derive the options, then confirm them here.

Run the workflow only after the choices are confirmed. Pair this with the export
guardrails below: show the resolved plan with `--dry-run`, get confirmation, then
run with `--export` for any task that writes data or starts a batch job.

## Output Artifacts

Every generated or revised GEE/geemap task should save:

```text
runs/YYYYMMDD-HHMM-task-name/
  code.py
  RUN.md
  DATA_LAYER.md
  sources.md
```

Use `templates/RUN.md` and `templates/DATA_LAYER.md` as starting points. Do not
overwrite old run folders.

## Coding Rules

- Write Python-first GEE code unless the user explicitly asks for JavaScript.
- Use explicit `import ee` and `import geemap` in runnable Python scripts.
- The saved script is named `code.py`, which shadows Python's standard-library
  `code` module that geemap loads through ipywidgets/IPython. Before importing
  geemap, drop the script directory from `sys.path[0]` so the stdlib module wins;
  otherwise `import geemap` crashes with
  `AttributeError: module 'code' has no attribute 'InteractiveConsole'`:

  ```python
  import sys
  from pathlib import Path
  if sys.path and Path(sys.path[0]).resolve() == Path(__file__).resolve().parent:
      sys.path.pop(0)
  import ee
  import geemap
  ```
- Initialize with `geemap.ee_initialize(project="PROJECT_ID")` or an equivalent
  `ee.Initialize(project="PROJECT_ID")` check before building ROI,
  ImageCollection, reducers, exports, or maps.
- Do not hard-code private project IDs, asset IDs, local paths, proxy
  credentials, tokens, or cookies. Use placeholders like `PROJECT_ID`,
  `ROI_ASSET`, `TILE_ASSET`, and `EXPORT_FOLDER`.
- For administrative boundaries use `FAO/GAUL/2015/level0|1|2` (filter on
  `ADM0_NAME` / `ADM1_NAME` / `ADM2_NAME`); GADM is not a public GEE asset. Names
  are romanized, e.g. China `ADM1_NAME="Sichuan Sheng"` — verify the exact string
  before relying on it. See `references/research_design.md`.
- If local vector data is provided, prefer `geemap.shp_to_ee`,
  `geemap.geojson_to_ee`, or `geemap.gdf_to_ee` after checking CRS, geometry
  type, and feature count.
- Keep heavy work server-side with Earth Engine objects. Avoid `.getInfo()` on
  large objects.
- Prefer `filterDate`, `filterBounds`, `select`, `map`, reducers, and exports
  over client-side loops.
- For exports, include `scale`, `region`, `crs`, and destination. Make exports
  opt-in: provide a `--dry-run` mode that prints the resolved parameters with no
  Earth Engine call, and only call `task.start()` when the user passes
  `--export`. Never auto-start an export on a plain run.
- Before exporting a multi-band image, cast every band to one dtype, e.g.
  `image.toFloat()`. Otherwise Earth Engine rejects the task with
  `Exported bands must have compatible data types; found inconsistent types:
  Float32 and Byte`. This happens whenever a float index band (NBR, dNBR, NDVI,
  recovery ratio) is combined with a Byte mask or class band produced by
  `.gte(...)`, `.selfMask()`, or `.toByte()`. Cast at export time, or export the
  integer/categorical products in a separate task that keeps their Byte dtype.
- For direct local GeoTIFF downloads, use `geemap.download_ee_image` only after
  checking that the region is small or controlled. Set `scale`, `region`,
  categorical/continuous dtype, resampling, and local output path deliberately.
  For large or complex ROIs, prefer Drive export, Asset export, or tiled export.
- Verify dataset IDs, band names, units, valid ranges, scale factors, offsets,
  nodata/mask values, QA bit rules, and dtype before numeric transformations.
- If a workflow may exceed Earth Engine limits, provide a tiled, batched, or
  intermediate-Asset pattern.

## Output Style

- Give complete runnable code with a short note about placeholders.
- Mention local file paths or source URLs when citing retrieved evidence.
- If local retrieval finds no direct support, say so and proceed from general
  Earth Engine/geemap knowledge or official online docs.
