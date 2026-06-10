# GEEMu Run Log

## User Goal

Build a Sentinel-2 NDVI median composite and export it for an ROI.

## Questions and Answers

- Cloud Project: `PROJECT_ID`
- Auth mode: `geemap.ee_initialize(project=PROJECT_ID)` and an Earth Engine online check before analysis/export
- Network/proxy: use `references/network_proxy.md` if authentication fails behind a restricted network
- Proxy host and port: not hard-coded
- Python or JavaScript: Python
- ROI source: `ROI_ASSET`
- Exact boundary required: yes for export; bbox is used for collection filtering
- Time range: `2024-06-01` to `2024-09-30`
- Dataset: `COPERNICUS/S2_SR_HARMONIZED`
- Scale: 10 m
- CRS: `EPSG:4326`
- Export target: Google Drive
- EECU/performance priority: moderate

## Parameter Ledger

| Item | Default/Old | Chosen/New | Reason | Effect/Risk |
|---|---|---|---|---|
| collection filter region | exact ROI | ROI bbox | faster coarse filtering | bbox may include extra scenes |
| cloud mask | none | SCL mask | remove cloud/shadow/snow | SCL is not perfect |
| output band | reflectance bands | NDVI | target vegetation index | range should remain `[-1, 1]` |

## Boundary Decision

- Boundary risk: medium until ROI complexity is known
- bbox strategy: use `roi.geometry().bounds(maxError=100)` for `filterBounds`
- simplify strategy: not applied by default
- tiling/grid: not needed unless export fails
- final region: exact ROI geometry

## EECU and Performance Strategy

- workload tag: `geemu-sentinel2-ndvi`
- filter order: `filterBounds`, `filterDate`, `select`, then `map`
- clip strategy: clip only final composite
- intermediate Asset export: not needed for this small template

## Assumptions and Risks

- ROI asset exists and user has access.
- Sentinel-2 SCL mask is acceptable for the task.
- Export scale and CRS should be reviewed for scientific use.
