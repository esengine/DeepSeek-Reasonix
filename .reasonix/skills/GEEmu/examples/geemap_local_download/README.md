# Example: geemap Local Download

This example shows how GEEMu should write a direct local GeoTIFF download using
`geemap.download_ee_image`.

It is adapted from a practical ESA WorldCover download pattern:

- initialize Earth Engine with a required Cloud Project ID
- define an ROI from an uploaded FeatureCollection asset or a lon/lat bbox
- select `ESA/WorldCover/v100` and the categorical `Map` band
- clip the image to the study region
- download directly to a local GeoTIFF with nearest-neighbor resampling
- keep generated `.tif` files out of Git

Use this pattern for small or controlled regions. For very large boundaries,
complex polygons, or high-resolution continental work, prefer tiled export,
Drive export, or intermediate Asset export.

## Required Placeholders

- `PROJECT_ID`: Earth Engine Cloud Project, or set `EE_PROJECT`, `GOOGLE_CLOUD_PROJECT`, or `EE_PROJECT_ID`
- `ROI_ASSET`: optional Earth Engine FeatureCollection asset
- `--bbox`: optional lon/lat rectangle if no uploaded ROI asset is available
- `output-dir`: local folder for GeoTIFF output

## Notes

Direct local download is convenient, but it is not always the safest export
strategy. Always check region size, scale, dtype, CRS, and whether the output is
categorical or continuous before downloading.
