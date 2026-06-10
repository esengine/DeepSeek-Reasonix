# Local Vector ROI Gate

Use this reference when the user provides a local line, polygon, shapefile,
GeoJSON, GeoPackage, or GeoDataFrame and wants to use it directly in Earth
Engine/geemap code.

## geemap Conversion Helpers

geemap can convert local vector data into Earth Engine objects:

```python
import geemap

roi = geemap.shp_to_ee("path/to/roi.shp")
roi = geemap.geojson_to_ee("path/to/roi.geojson")
roi = geemap.gdf_to_ee(gdf)
```

These are useful when the local boundary should be used directly as an
`ee.FeatureCollection` without first uploading it as a permanent Earth Engine
Asset.

Local evidence:

- `geemap/usage.md` shows `geemap.shp_to_ee(shp_file_path)` and
  `geemap.geojson_to_ee(geojson_file_path)`.
- `geemap/workshops/AGU_2024.ipynb` shows `geemap.gdf_to_ee(gdf)`.

## Checks Before Conversion

Before using local vector data:

- confirm file path and format
- confirm geometry type: point, line, polygon, or mixed
- confirm CRS; reproject to EPSG:4326 if needed
- check feature count and geometry complexity
- check whether line geometries need buffering before image reduction/export
- check whether exact boundary, bbox, simplify, or tile grid should be used
- decide whether the data should remain local-converted or be uploaded as an
  Earth Engine Asset for repeat use

## Line Geometry Rule

If the user provides a local line layer, do not use the line directly as an image
export region. Ask for or choose a buffer distance, then convert buffered geometry
to a polygon ROI.

Example:

```python
import geopandas as gpd
import geemap

gdf = gpd.read_file("path/to/lines.shp").to_crs("EPSG:4326")
buffered = gdf.to_crs("EPSG:3857")
buffered["geometry"] = buffered.geometry.buffer(100)
buffered = buffered.to_crs("EPSG:4326")

roi = geemap.gdf_to_ee(buffered)
```

## When to Prefer Asset Upload

Prefer uploading to an Earth Engine Asset when:

- the vector file is large or complex
- it will be reused across many scripts
- multiple users or machines need the same ROI
- conversion makes scripts slow or brittle
- the workflow should be fully reproducible without local files

Record the choice in `RUN.md`.
