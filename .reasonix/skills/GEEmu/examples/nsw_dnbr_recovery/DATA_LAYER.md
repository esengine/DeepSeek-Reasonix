# Data Layer

## Data Need

- Target variable: burned area and vegetation recovery
- Spatial extent: New South Wales, Australia
- Time range: pre-fire, post-fire, and recovery seasonal windows
- Spatial resolution: 100 m
- Temporal resolution: seasonal median composite
- Output target: summary JSON, optional GeoTIFF exports
- Official-only or community allowed: official datasets only

## Dataset Candidates

| Source | Dataset ID | Title | Provider | Type | Why it fits | Limits | License | Verification |
|---|---|---|---|---|---|---|---|---|
| Official | `FAO/GAUL/2015/level1` | GAUL level 1 administrative units | FAO/GAUL | FeatureCollection | provides New South Wales boundary | admin boundary may differ from local authoritative data | check GEE catalog | property pattern from local docs |
| Official | `LANDSAT/LC08/C02/T1_L2` | Landsat 8 Collection 2 Level 2 | USGS/NASA | ImageCollection | NBR from NIR/SWIR2 | cloud/shadow masking required | check GEE catalog | band logic verified |
| Official | `LANDSAT/LC09/C02/T1_L2` | Landsat 9 Collection 2 Level 2 | USGS/NASA | ImageCollection | recovery window continuity | not available for pre/post 2019-2020 | check GEE catalog | band logic verified |

## Selected Dataset

| Band/Field | Meaning | Unit | Raw range | Scale/Offset | Valid range | Mask/QA | Notes |
|---|---|---|---|---|---|---|---|
| `SR_B5` | NIR reflectance | scaled reflectance | integer | `* 0.0000275 - 0.2` | reflectance | `QA_PIXEL`, `QA_RADSAT` | NBR input |
| `SR_B7` | SWIR2 reflectance | scaled reflectance | integer | `* 0.0000275 - 0.2` | reflectance | `QA_PIXEL`, `QA_RADSAT` | NBR input |
| `NBR` | Normalized Burn Ratio | unitless | derived | `(NIR - SWIR2) / (NIR + SWIR2)` | `[-1, 1]` | inherits mask | recovery metric |
| `dNBR` | burn detection index | unitless | derived | `pre_NBR - post_NBR` | task-dependent | thresholded | detects burned area |
| `burned_mask` | detected burned area | binary | derived | `dNBR >= 0.20` | 1 where burned | selfMask | controls recovery ROI |
| `NBR_recovery_ratio` | NBR-based recovery | ratio | derived | `(recovery_NBR - post_NBR) / dNBR` | clamped `0-1.5` | burned mask | use NBR instead of NDVI |

## Transformations

| Step | Input | Formula | Output meaning | Output range | Dtype | Risk |
|---|---|---|---|---|---|---|
| Scale SR | `SR_B.*` | `value * 0.0000275 - 0.2` | surface reflectance | reflectance | float | NBR wrong if raw DN is used |
| NBR | `SR_B5`, `SR_B7` | `(NIR - SWIR2) / (NIR + SWIR2)` | vegetation/burn signal | `[-1, 1]` | float | residual cloud/shadow |
| dNBR | pre/post NBR | `pre - post` | burn detection | positive burn signal | float | threshold needs calibration |
| Burn mask | dNBR | `dNBR >= 0.20` | burned area | binary | byte/mask | omission/commission risk |
| NBR recovery | post/recovery NBR | `(recovery - post) / dNBR` | relative NBR recovery | `0-1.5` | float | unstable where dNBR small |

## Semantic Checks

- Scale factor verified: yes, local GEE Landsat C2 examples
- Offset verified: yes, local GEE Landsat C2 examples
- QA mask verified: `QA_PIXEL` and `QA_RADSAT`
- Projection/CRS verified: export uses `EPSG:4326`
- Physical range preserved: yes
- Storage scaling reversible: no integer storage scaling applied
