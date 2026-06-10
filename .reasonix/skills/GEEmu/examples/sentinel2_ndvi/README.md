# Example: Sentinel-2 NDVI Composite

This example shows the standard GEEMu pattern for a Sentinel-2 NDVI workflow.

It demonstrates:

- Earth Engine Python authentication with `PROJECT_ID`
- optional local proxy awareness
- data semantics for Sentinel-2 bands and NDVI
- bbox-first filtering and exact ROI export
- a simple EECU-aware export task
- `RUN.md`, `DATA_LAYER.md`, and `sources.md` traceability

## Files

- `code.py`: runnable template after replacing placeholders
- `RUN.md`: run log example
- `DATA_LAYER.md`: data semantics record
- `sources.md`: local evidence checklist

## Required Placeholders

- `PROJECT_ID`: your Earth Engine Cloud Project, or set `EE_PROJECT`, `GOOGLE_CLOUD_PROJECT`, or `EE_PROJECT_ID`
- `ROI_ASSET`: your ROI FeatureCollection asset
- `EXPORT_FOLDER`: Google Drive folder for exports

## Run

```powershell
python examples\sentinel2_ndvi\code.py
```
