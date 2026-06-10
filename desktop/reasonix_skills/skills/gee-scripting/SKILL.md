---
name: gee-scripting
description: Google Earth Engine scripting workflow guide for remote sensing analysis. Read this when the task involves GEE — image filtering, compositing, classification, change detection, or any ee.Image/ee.FeatureCollection operations.
runAs: inline
---

You are assisting with Google Earth Engine scripting in RS-Reasonix.
All GEE scripts execute via `mcp__geocode__run_gee_script`.

## Script Mode

Two modes available via `mcp__geocode__run_gee_script`:

| Parameter | Usage |
|-----------|-------|
| `script` | Inline Python code, passed as a string. Best for short (<100 line) scripts. |
| `script_path` | Path to a `.py` file on disk. Use when the script is already saved. |

## Standard Script Structure

```python
from internal.geo.mcp_server.tools.geocode import init_gee, heartbeat, load_region

# 1. Initialize
init_gee()

# 2. Define region (from local boundary file, not GEE built-in datasets)
region = load_region("/path/to/boundary.shp")

# 3. Filter + composite image collection
collection = (
    ee.ImageCollection("COPERNICUS/S2_SR_HARMONIZED")
    .filterBounds(region)
    .filterDate("2024-06-01", "2024-08-31")
    .filter(ee.Filter.lt("CLOUDY_PIXEL_PERCENTAGE", 30))
)
image = collection.median().clip(region)

# 4. Compute
ndvi = image.normalizedDifference(["B8", "B4"]).rename("NDVI")

# 5. Output — always use print() for results
stats = ndvi.reduceRegion(
    reducer=ee.Reducer.mean(),
    geometry=region,
    scale=10,
    maxPixels=1e13
)
print(stats.getInfo())
```

## Heartbeat

Long-running GEE tasks (>30s) MUST call `heartbeat()` in loops to prevent timeout:

```python
for i, image in enumerate(collection_list):
    result = process_image(image)
    if i % 10 == 0:
        heartbeat()
    print(f"Processed {i+1}/{total}")
```

## geocode Helper Functions

Available from `internal.geo.mcp_server.tools.geocode`:

| Function | Purpose |
|----------|---------|
| `init_gee()` | Initialize Earth Engine with configured project |
| `load_region(path)` | Load a boundary from .shp/.geojson as an ee.Geometry |
| `check_coverage(region, start, end, collection)` | Verify image availability before processing |
| `download_image(image, region, scale)` | Export an ee.Image to local GeoTIFF (best for small areas) |
| `export_image(image, region, scale, bucket=None)` | Export to GEE Asset or Google Drive (best for large areas) |

## When to download vs export

- **download_image**: <100 km², single image, quick check
- **export_image**: large area, multi-band, batch processing

## Key Rules

1. **Prepare boundary files locally** — do NOT use GEE built-in boundary datasets (`FAO/GAUL`, `USDOS/LSIB`). Create `.shp` or `.geojson` first with QGIS or `mcp__geocode__run_qgis_algorithm`.
2. **maxPixels=1e13** — always set this on reduceRegion/reduceRegions calls to avoid "computed too many pixels" errors.
3. **Use `mcp__geocode__read_geo_data`** to verify downloaded images (CRS, extent, nodata).
4. **Prefer `median()` over `mosaic()`** for multi-temporal composites — it handles outliers better.

## Reference Documents

Detailed guides in the `references/` directory:

| Document | Content |
|----------|---------|
| `references/image-composite.md` | Image filtering, cloud masking, compositing methods (469 lines) |
| `references/classification.md` | Classifier selection, training samples, accuracy assessment (437 lines) |
| `references/change-detection.md` | Differencing, ratio, post-classification, continuous change (356 lines) |

## Templates

Ready-to-use scripts in the `templates/` directory:

| Template | Purpose |
|----------|---------|
| `templates/single-date-image.md` | Best single-date image selection by cloud cover ranking |
