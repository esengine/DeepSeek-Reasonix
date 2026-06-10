# Boundary and Compute Gate

Use this reference when an ROI, boundary, export region, reducer, tiling plan, memory risk, timeout risk, or EECU concern affects the workflow.

## Boundary Checks

Before expensive operations, consider:

- ROI area
- bounding box area
- whether the bbox greatly exceeds the true ROI
- vertex count and geometry detail
- multipart geometry
- holes
- long thin shapes
- whether exact boundaries matter for the final result

Record boundary risk as `low`, `medium`, or `high` in `RUN.md`.

## Practical Rules

- Prefer `filterBounds(roi.bounds())` for coarse filtering when appropriate.
- Use the exact ROI for final statistics, masks, or export regions when exact boundaries matter.
- Avoid repeated early `clip()` unless it is needed for correctness or export size.
- Consider `simplify`, `bounds`, grid/tile splitting, or intermediate Asset exports when boundary risk is high.
- Avoid `FeatureCollection.geometry()` on very large collections if a collection-aware workflow is available.

## Tiling and Divisibility

Only split work into tiles when the algorithm is tile-safe.

Usually tile-safe:

- per-pixel index computation
- per-pixel time-series trends
- independent export patches
- local neighborhood operations with enough buffer

Use caution:

- PCA or RSEI with global normalization
- reducers that require global statistics
- connected components or region-growing workflows
- classifications where training/sample distribution changes by tile

## EECU and Performance Notes

- Earth Engine cost depends on dataset, region, scale, reducer, mask density, export strategy, and task graph complexity.
- Use `filterDate`, `filterBounds`, and `select` early.
- Keep computation server-side and avoid large `.getInfo()` calls.
- Put expensive `.map()` operations after coarse filtering.
- Export reusable intermediate results to Assets when repeated downstream work would recompute the same graph.
- Use smaller dtypes only when data semantics allow it.
- For Python, use workload tags when monitoring jobs:

```python
ee.data.setDefaultWorkloadTag("geemu-task")
# ee.data.setWorkloadTag("export-jobs")
# ee.data.resetWorkloadTag()
```
