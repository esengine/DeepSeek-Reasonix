# GEEMu Run Log

## User Goal

Export a large NDVI image by tiles instead of one large region.

## Questions and Answers

- Cloud Project: `PROJECT_ID`
- Auth mode: `geemap.ee_initialize(project=PROJECT_ID)` and an Earth Engine online check before export
- Network/proxy: use `references/network_proxy.md` if authentication fails
- Proxy host and port: not hard-coded
- Python or JavaScript: Python
- ROI source: `ROI_ASSET`
- Tile source: `TILE_ASSET`
- Exact boundary required: tile regions are exact export regions
- Dataset: `COPERNICUS/S2_SR_HARMONIZED`
- Scale: 10 m
- CRS: `EPSG:4326`
- Export target: Google Drive

## Boundary Decision

- Boundary risk: high if one-shot export fails
- bbox strategy: filter imagery by ROI bbox
- simplify strategy: not used
- tiling/grid: user-provided tile FeatureCollection
- final region: each tile geometry

## EECU and Performance Strategy

- workload tag: `geemu-tiled-export`
- tile count guard: `MAX_TILES`
- algorithm tile-safety: NDVI median composite is per-pixel and tile-safe for export
- client-side calls: only tile count and task creation

## Assumptions and Risks

- The tile grid fully covers the ROI.
- Tile count is small enough for task management.
- This pattern is not automatically valid for global PCA/RSEI normalization.
