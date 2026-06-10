# Data Layer

## Data Need

- Target variable: land-cover class
- Spatial extent: ROI asset or lon/lat bbox
- Time range: 2020 WorldCover product
- Spatial resolution: 10 m
- Temporal resolution: annual product
- Output target: local GeoTIFF
- Official-only or community allowed: official dataset

## Dataset Candidates

| Source | Dataset ID | Title | Provider | Type | Why it fits | Limits | License | Verification |
|---|---|---|---|---|---|---|---|---|
| Official | `ESA/WorldCover/v100` | ESA WorldCover 10 m 2020 | ESA via GEE | ImageCollection | global 10 m land-cover map | categorical classes, single-year product | check GEE catalog | band name and class table should be verified |

## Selected Dataset

| Band/Field | Meaning | Unit | Raw range | Scale/Offset | Valid range | Mask/QA | Notes |
|---|---|---|---|---|---|---|---|
| `Map` | land-cover class code | class ID | categorical integers | none | check catalog class table | image mask | convert to byte only when class codes fit |

## Transformations

| Step | Input | Formula | Output meaning | Output range | Dtype | Risk |
|---|---|---|---|---|---|---|
| Select band | WorldCover image | `.select("Map")` | land-cover class map | class IDs | categorical | wrong band breaks output |
| Cast | selected image | `.toByte()` | compact class storage | class IDs | uint8 | unsafe if adapting to non-byte datasets |
| Clip | image and ROI | `.clip(region)` | class map inside study area | class IDs | uint8 | complex ROI can increase download cost |
| Local download | clipped image | `geemap.download_ee_image` | local GeoTIFF | class IDs | uint8 | large downloads can fail or time out |

## Semantic Checks

- Class table verified: check official catalog before publication
- Resampling: nearest neighbor for categorical class data
- Scale: 10 m
- CRS: native unless the user provides `--crs`
- Physical range preserved: yes for categorical IDs
- Storage scaling reversible: no scaling applied
