# Example: Tiled Export

This example shows a tiled export pattern for larger Earth Engine workflows.

It demonstrates:

- using a user-provided tile grid FeatureCollection
- bbox-first filtering
- exact tile regions for export
- bounded client-side task creation
- recording whether the algorithm is tile-safe

The example uses a Sentinel-2 NDVI composite, but the tiled export pattern can be
adapted to other per-pixel images.

## Required Placeholders

- `PROJECT_ID`: Earth Engine Cloud Project, or set `EE_PROJECT`, `GOOGLE_CLOUD_PROJECT`, or `EE_PROJECT_ID`
- `ROI_ASSET`: ROI FeatureCollection
- `TILE_ASSET`: FeatureCollection of export tiles
- `EXPORT_FOLDER`: Google Drive export folder

## Run

```powershell
python examples\tiled_export\code.py
```
