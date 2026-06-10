# GEEMu Run Log

## User Goal

Download ESA WorldCover 2020 land-cover data directly from Earth Engine to a
local GeoTIFF using geemap.

## Questions and Answers

- Cloud Project: `PROJECT_ID`
- Auth mode: `geemap.ee_initialize(project=PROJECT_ID)` and an Earth Engine online check before download
- Network/proxy: use `references/network_proxy.md` if authentication fails behind a restricted network
- Proxy host and port: not hard-coded
- Python or JavaScript: Python
- ROI source: `ROI_ASSET` or `--bbox WEST SOUTH EAST NORTH`
- Exact boundary required: yes for final download region
- Time range: ESA WorldCover v100 first image, representing 2020
- Dataset: `ESA/WorldCover/v100`
- Scale: 10 m
- CRS: native unless explicitly set
- Export target: local GeoTIFF
- EECU/performance priority: low to medium for small regions; high risk for large regions

## Parameter Ledger

| Item | Default/Old | Chosen/New | Reason | Effect/Risk |
|---|---|---|---|---|
| output target | Drive/Asset export | local GeoTIFF | direct local delivery requested | unsuitable for very large regions |
| resampling | unspecified | nearest neighbor | categorical land-cover class | avoids invalid class interpolation |
| dtype | unspecified | uint8 | WorldCover class codes fit integer class storage | verify if adapting to other datasets |
| region | private asset path | placeholder ROI asset or bbox | public example must not hard-code private assets | user must provide a real ROI |

## Boundary Decision

- Boundary risk: medium until ROI size and vertex count are known
- bbox strategy: accepted for simple rectangular tests
- simplify strategy: not applied by default
- tiling/grid: recommended if local download is too large
- final region: exact ROI asset geometry or bbox rectangle

## EECU and Performance Strategy

- workload tag: `geemu-local-download`
- filterBounds: not needed for single WorldCover image
- map/reduce order: not applicable
- clip strategy: clip final selected image to ROI
- dtype/storage strategy: `toByte()` and `dtype="uint8"` for categorical output
- intermediate Asset export: not needed for small ROI; recommended for large repeated work

## Assumptions and Risks

- The ROI asset exists and the user has access, or the user passes a valid bbox.
- Direct local download may fail for large or complex regions.
- ESA WorldCover classes are categorical; do not use bilinear/cubic resampling.
