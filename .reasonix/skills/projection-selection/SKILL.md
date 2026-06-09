---
name: projection-selection
description: Select an appropriate projected coordinate system for geographic data analysis or cartography. Read this when you need to determine which projection to use. Before using, first identify the study area extent — zone numbers, standard parallels, and central meridians all depend on location and extent.
runAs: inline
---

You are selecting a projected coordinate system for a GIS/remote sensing task in RS-Reasonix.

## Decision Framework: Two Dimensions

### Dimension 1: Distortion Property (What do you need to preserve?)

| Property | Choose | Best for |
|----------|--------|----------|
| **Angles / Shapes** | Conformal | Navigation, boundary matching, large-scale engineering |
| **Area** | Equal-Area | Land cover statistics, density mapping, area calculations |
| **Distances** | Equidistant | Distance measurements from a point or along a line |
| **Balance** | Compromise | General-purpose display, world maps |

**Key principle**: Equal-area and conformal are *mutually exclusive* — choose ONE based on the primary analysis goal. At small scales (city/county level), distortion is negligible regardless of projection choice.

### Dimension 2: Geographic Extent (Where is the study area?)

| Extent | Recommended Family | Examples |
|--------|-------------------|----------|
| **World** | Pseudocylindrical | Equal Earth, Robinson, Natural Earth |
| **E-W elongated** (e.g., continent at low latitude) | Cylindrical | Mercator, Plate Carrée |
| **E-W elongated** (mid-latitude) | Conic | Albers Equal-Area Conic, Lambert Conformal Conic |
| **N-S elongated** (e.g., Africa, South America) | Cylindrical (Transverse) | Transverse Mercator |
| **Circular** (e.g., polar region) | Azimuthal | Stereographic, Lambert Azimuthal Equal-Area |
| **Country / Province** (mid-latitude) | Conic | Albers, Lambert Conformal, Equidistant Conic |

## GIS Analysis vs. Cartography

**GIS Analysis**: Let the task dictate the projection. Area stats → equal-area. Distance → equidistant. Shape analysis → conformal. Use different projections for different analysis phases — there is no single "correct" projection.

**Cartography**: Follow these rules:
1. **National standard first** — check local mapping conventions (e.g., China → CGCS2000)
2. **Thematic appropriateness** — population density → equal-area; air routes → azimuthal equidistant
3. **Reader familiarity** — use commonly recognized projections for the region

## Common Quick Reference

| Scenario | Projection | EPSG |
|----------|------------|------|
| Global thematic (equal-area) | Equal Earth | 8857 |
| Global thematic (compromise) | Robinson | 54030 |
| Global web map | Web Mercator | 3857 |
| Continental (S. America, Africa) | Albers Equal-Area Conic | vary by params |
| National (China) | Albers Equal-Area Conic (110°E, 25°N, 47°N) | — |
| National (China, official) | CGCS2000 / Gauss-Kruger | 4490 |
| Local (<100km²) | UTM (appropriate zone) | 326xx / 327xx |

## When Working in RS-Reasonix

1. Always verify current CRS with `mcp__geocode__read_geo_data` before reprojecting.
2. For QGIS processing, use `mcp__geocode__qgis_doc` to find the correct reprojection algorithm parameters.
3. For batch reprojection, use `mcp__geocode__run_qgis_algorithm` with `native:reprojectlayer` or `gdal:warpreproject`.
4. After reprojection: verify output CRS with `mcp__geocode__read_geo_data`.

## Reference

**`references/中国制图投影坐标系规范.md`** — Read whenever the study area involves any part of China. Covers:
- CGCS2000 national standards
- Standard parallel and central meridian rules by province
- Gauss-Kruger 3-degree / 6-degree zone selection
- Worked examples for city/county-level mapping
