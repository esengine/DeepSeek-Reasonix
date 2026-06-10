# Data Layer

## Data Need

- Target variable: NDVI
- Spatial extent: ROI asset
- Time range: `2024-06-01` to `2024-09-30`
- Spatial resolution: 10 m
- Temporal resolution: median over date range
- Output target: Google Drive GeoTIFF
- Official-only or community allowed: official dataset

## Dataset Candidates

| Source | Dataset ID | Title | Provider | Type | Why it fits | Limits | License | Verification |
|---|---|---|---|---|---|---|---|---|
| Official | `COPERNICUS/S2_SR_HARMONIZED` | Sentinel-2 MSI surface reflectance | ESA/Copernicus via GEE | ImageCollection | 10 m red/NIR bands for NDVI | cloud/shadow masking required | check GEE catalog | band names verified |

## Selected Dataset

| Band/Field | Meaning | Unit | Raw range | Scale/Offset | Valid range | Mask/QA | Notes |
|---|---|---|---|---|---|---|---|
| `B4` | red reflectance | scaled reflectance | check catalog | check catalog | non-negative reflectance | SCL mask | used in NDVI |
| `B8` | near infrared reflectance | scaled reflectance | check catalog | check catalog | non-negative reflectance | SCL mask | used in NDVI |
| `SCL` | scene classification | class code | categorical | none | class IDs | masks cloud/shadow/snow | exclude 3, 8, 9, 10, 11 |
| `NDVI` | vegetation index | unitless | derived | `(B8 - B4) / (B8 + B4)` | `[-1, 1]` | inherits mask | float output |

## Transformations

| Step | Input | Formula | Output meaning | Output range | Dtype | Risk |
|---|---|---|---|---|---|---|
| NDVI | `B8`, `B4` | normalized difference | vegetation greenness | `[-1, 1]` | float | invalid if bands are misidentified |
| Median composite | NDVI collection | median over time | seasonal central tendency | `[-1, 1]` | float | residual cloud can affect median |

## Semantic Checks

- Scale factor verified: check official catalog before publication
- Offset verified: check official catalog before publication
- QA mask verified: SCL class mask used
- Projection/CRS verified: export uses `EPSG:4326`
- Physical range preserved: yes
- Storage scaling reversible: no integer scaling applied
