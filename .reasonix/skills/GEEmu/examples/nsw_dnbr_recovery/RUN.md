# GEEMu Run Log

## User Goal

Use New South Wales as the ROI, detect burned area with dNBR, then analyze
vegetation recovery using NBR in the detected burned area.

## Questions and Answers

- Cloud Project: required at runtime through `--project PROJECT_ID`
- Auth mode: local credentials can be reused, but `geemap.ee_initialize(project=PROJECT_ID)` and an Earth Engine online check must pass before analysis
- Network/proxy: use `references/network_proxy.md` if initialization fails
- Python or JavaScript: Python
- ROI source: `FAO/GAUL/2015/level1`, New South Wales
- Exact boundary required: yes, but `filterBounds(roi.bounds())` is used for image collection filtering
- Dataset: Landsat 8/9 Collection 2 Level 2
- Scale: `SCALE = 100`
- CRS: `EPSG:4326`
- Export target: optional Google Drive exports

## Parameter Ledger

| Item | Default/Old | Chosen/New | Reason | Effect/Risk |
|---|---|---|---|---|
| region | unspecified placeholder | New South Wales | user specified NSW | full-state computation is larger |
| scale | 30 m | 100 m | user requested lower resolution for actual run | faster, less spatial detail |
| fire detection | external fire boundary | dNBR threshold | detect burned area from imagery | threshold controls omission/commission |
| recovery metric | generic recovery | NBR recovery ratio | user requested NBR replacement | sensitive to seasonal windows |

## Boundary Decision

- Boundary risk: medium/high because NSW is a large polygon
- bbox strategy: use NSW bbox for collection filtering
- simplify strategy: not applied
- tiling/grid: recommended if export or summary times out
- final region: NSW exact geometry

## EECU and Performance Strategy

- workload tag: `geemu-nsw-dnbr-recovery`
- scale: 100 m
- filter order: ROI bbox, date, QA mask, NBR, median
- burned-area mask: `dNBR >= 0.20`
- recovery analysis: masked to burned area only
- exports: optional, after summary

## Assumptions and Risks

- The 2019-2020 NSW fire season is represented by the chosen post-fire window.
- dNBR threshold `0.20` is a practical test threshold, not a calibrated local severity threshold.
- Full NSW at 100 m is still a substantial Earth Engine task.
