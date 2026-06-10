# Research Design Gate

Use this reference before dataset selection or code generation when the task is
open-ended, scientific, or could be solved at multiple regions, scales, or output
levels.

GEEMu should anchor every GEE/geemap workflow with three decisions:

1. Study area: where the analysis happens and how the ROI is represented.
2. Analysis scale: the computation resolution, projection, temporal window, and
   whether the chosen scale is compatible with the selected dataset.
3. Output target: what the user needs at the end, such as map layers, summary
   statistics, time series, GeoTIFF exports, Asset exports, tables, or figures.

## Use Local Knowledge for Recommendations

When any of the three decisions is missing or vague, search the local knowledge
bundle before choosing defaults:

```powershell
python _gee_vector_db.py search "study area resolution output target <task keywords>" --db gee_vector_db --limit 8
```

Then check the structured catalog if dataset choice is involved:

```powershell
rg -n "<keyword>" awesome-gee-community-datasets/community_datasets.csv
```

Use the local knowledge to produce a recommendation table, not only a single
answer. Include:

- recommendation: the preferred study area, analysis scale, or output target
- reason: why it fits the science question and the data
- limit: what can go wrong
- required confirmation: what the user must confirm before expensive compute

## Study Area Checks

Record:

- ROI source: administrative boundary, asset, local vector file, drawn geometry,
  bounding box, point buffer, or generated grid
- exactness need: exact boundary, bbox approximation, or buffered analysis
- geometry risk: feature count, vertex complexity, holes, multipart geometry,
  line-to-polygon buffer need
- cross-border or water/land masking concerns

If the study area is large or complex, propose a bbox-first filter and exact ROI
for final statistics/export. If the user only gives a place name, suggest 2-3
boundary sources and ask for confirmation when the choice changes meaning.

### Administrative boundary sources

For administrative units, default to **FAO GAUL 2015** in the Earth Engine
catalog: `FAO/GAUL/2015/level0` (country), `level1` (province/state), `level2`
(prefecture / county level). **GADM is not available as a public GEE asset**, so
do not try `ee.FeatureCollection("GADM/...")`. Filter GAUL on `ADM0_NAME`,
`ADM1_NAME`, or `ADM2_NAME`. Names are GAUL's romanized forms, e.g. for China:
`ADM0_NAME = "China"`, `ADM1_NAME = "Sichuan Sheng"`, `ADM2_NAME =
"Liangshan Yi Zu Zizhizhou"`. Spelling varies, so verify the exact string with a
quick `.filter(...).size()` or `.aggregate_array("ADM2_NAME")` before relying on
it. For boundaries finer than GAUL level 2, ask the user for an uploaded asset or
a local vector file instead.

## Analysis Scale Checks

Record:

- requested scale and unit
- dataset native or nominal scale
- reducer/export scale
- CRS/projection choice
- temporal aggregation window
- whether scale is for exploration, final analysis, or export only

Do not silently use a scale finer than the selected data can support. Prefer:

- native scale for final pixel-level interpretation
- coarser scale for large-area exploratory summaries
- explicit aggregation when mixing datasets with different resolutions

If cost is a concern, propose a lower-resolution test run first and mark it as a
test scale rather than the final scientific scale.

## Output Target Checks

Record the final object before writing code:

- quick map preview
- numeric summary JSON
- CSV/table
- time series
- GeoTIFF
- Earth Engine Asset
- Google Drive export
- reusable Python function/notebook

Match computation strategy to the output. For example, summary statistics can use
reducers and coarse test scale, while publishable raster outputs need explicit
`scale`, `region`, `crs`, mask handling, dtype/storage strategy, and export
destination.

## Minimum RUN.md Entry

```markdown
## Research Design

| Decision | Choice | Suggested by local knowledge | Reason | Needs confirmation |
|---|---|---|---|---|
| Study area |  |  |  |  |
| Analysis scale |  |  |  |  |
| Output target |  |  |  |  |
```

If a decision is assumed, write `ASSUMED` and explain why. If the local knowledge
does not provide support, write `not found` and proceed from general Earth
Engine/geemap judgment.
