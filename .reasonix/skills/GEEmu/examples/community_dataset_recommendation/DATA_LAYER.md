# Data Layer

## Data Need

- Target variable: precipitation/rainfall/climate candidate datasets
- Spatial extent: not specified
- Time range: not specified
- Spatial resolution: unknown until dataset is selected
- Temporal resolution: unknown until dataset is selected
- Output target: candidate table
- Official-only or community allowed: community allowed

## Community CSV Lookup

- CSV path: `awesome-gee-community-datasets/community_datasets.csv`
- Search terms: precipitation, rainfall, climate
- Matched fields: `title`, `tags`, `thematic_group`, `provider`, `type`
- Selected row IDs: generated in `community_candidates.csv`

## Dataset Candidates

| Source | Dataset ID | Title | Provider | Type | Why it fits | Limits | License | Verification |
|---|---|---|---|---|---|---|---|---|
| Community | from CSV output | from CSV output | from CSV output | from CSV output | keyword match | semantics unknown | per row | docs page required |

## Semantic Checks

- Scale factor verified: UNKNOWN
- Offset verified: UNKNOWN
- QA mask verified: UNKNOWN
- Projection/CRS verified: UNKNOWN
- Physical range preserved: UNKNOWN
- Storage scaling reversible: UNKNOWN

## Open Questions

- What exact climate variable is needed?
- Is an official GEE catalog dataset preferred?
- What spatial and temporal resolution are required?
