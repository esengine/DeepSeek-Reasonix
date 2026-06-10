# Data Layer

## Data Need

- Target variable: NDVI
- Spatial extent: ROI split by tile grid
- Time range: `2024-06-01` to `2024-09-30`
- Spatial resolution: 10 m
- Output target: one GeoTIFF per tile

## Dataset Candidates

| Source | Dataset ID | Title | Provider | Type | Why it fits | Limits | License | Verification |
|---|---|---|---|---|---|---|---|---|
| Official | `COPERNICUS/S2_SR_HARMONIZED` | Sentinel-2 surface reflectance | ESA/Copernicus via GEE | ImageCollection | red and NIR bands for NDVI | clouds require masking | check GEE catalog | band names verified |

## Selected Dataset

| Band/Field | Meaning | Unit | Raw range | Scale/Offset | Valid range | Mask/QA | Notes |
|---|---|---|---|---|---|---|---|
| `B4` | red reflectance | scaled reflectance | check catalog | check catalog | non-negative reflectance | SCL mask | used in NDVI |
| `B8` | near infrared reflectance | scaled reflectance | check catalog | check catalog | non-negative reflectance | SCL mask | used in NDVI |
| `SCL` | scene classification | class code | categorical | none | class IDs | cloud/shadow/snow mask | exclude 3, 8, 9, 10, 11 |
| `NDVI` | vegetation index | unitless | derived | `(B8 - B4) / (B8 + B4)` | `[-1, 1]` | inherits mask | tile-safe per-pixel output |

## Transformations

| Step | Input | Formula | Output meaning | Output range | Dtype | Risk |
|---|---|---|---|---|---|---|
| NDVI | `B8`, `B4` | normalized difference | vegetation greenness | `[-1, 1]` | float | residual clouds |
| Tiled export | NDVI image | export by tile geometry | same physical NDVI | `[-1, 1]` | float | tile seams only from export boundaries |
