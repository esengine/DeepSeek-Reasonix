---
name: thematic-map
description: Create professional thematic maps with Cartopy, Matplotlib, and frykit. Use this skill when you need to compose a map. Complete all data preparation and processing BEFORE reading this skill — only invoke it when the data is fully ready and you're about to compose the final map figure.
runAs: inline
---

You are creating a professional thematic map in RS-Reasonix using the Python Cartopy + Matplotlib + frykit stack.

## Environment

Required packages: `cartopy`, `matplotlib`, `frykit` (includes frykit[data] for China boundaries), `shapely`, `rasterio`, `numpy`.

## Mapping Workflow

```
1. Understand requirements   → What to show? Who is the audience? Print or digital?
2. Confirm environment       → Are Cartopy/frykit importable?
3. Write code                → Follow the code standards below
4. Run and self-review       → Use the image review checklist
5. Deliver output            → PNG (300 dpi) for digital; PDF/SVG for print
```

## Core Concept: projection vs transform

Cartopy's key distinction:
- `projection` = the map's canvas coordinate system (how data is displayed)
- `transform` = the data's native coordinate system (how data is stored)

```python
import cartopy.crs as ccrs

fig, ax = plt.subplots(subplot_kw={"projection": ccrs.AlbersEqualArea(105, 25, 47)})
ax.contourf(lons, lats, data, transform=ccrs.PlateCarree())  # data is WGS84
```

Data is ALWAYS in WGS84 (`ccrs.PlateCarree()`) unless you've reprojected it — this is the most common mistake.

## Map Element Checklist

### Required Elements

| Element | Requirement |
|---------|-------------|
| **Title** | Descriptive, fontsuze ≥14, located at top |
| **Data Layer** | Correct CRS, proper colormap, non-missing data handled |
| **Legend / Colorbar** | All classes labeled, units shown, consistent with data range |
| **Attribution** | Data source, satellite, date range |

### Recommended Elements

| Element | Requirement |
|---------|-------------|
| **Graticules** | Latitude/longitude lines, dms format, readable spacing |
| **Scale Bar** | Actual geographic distance, integer values, metric units |
| **North Arrow** | Present, correctly oriented |

### Optional Elements

| Element | Requirement |
|---------|-------------|
| **Inset Map** | e.g., South China Sea inset for China maps |
| **Basemap** | Coastlines, country borders, terrain shading |
| **Location Map** | Show study area in broader context |

## Image Review Checklist

Before delivering a map, verify:

- [ ] Title is present, descriptive, and correctly sized
- [ ] Data layer displays correctly (no missing patches, correct CRS)
- [ ] Legend/colorbar is complete with units
- [ ] Scale bar shows actual geographic distances
- [ ] North arrow is present and oriented correctly
- [ ] Graticules/tick marks are readable and properly spaced
- [ ] Overall layout: no clipped elements, balanced whitespace, appropriate figure size
- [ ] No Chinese character rendering issues (font fallback working)

## Code Standards

1. **Always set figure DPI** — `plt.figure(dpi=300)` for production output.
2. **Explicit `transform=ccrs.PlateCarree()`** on every data-adding call.
3. **Use `ax.set_extent()`** to crop to study area before adding expensive basemap layers.
4. **Close figures** — `plt.close()` after saving to free memory in batch jobs.
5. **Use frykit helpers** — `set_map_ticks()`, `add_compass()`, `add_scale_bar()` for professional formatting.

## Reference Documents

| Document | Content |
|----------|---------|
| `references/basemap.md` | Cartopy basemap configuration — built-in features + online tile maps (627 lines) |
| `references/cartopy-projection.md` | All 37 Cartopy `ccrs` projections with China-specific configs (878 lines) |
| `references/frykit.md` | Frykit map tools: compass, scale bar, inset maps, color tools (669 lines) |

## Templates

| Template | Content |
|----------|---------|
| `templates/中国东北三省地图示例.md` | Northeast China terrain slope map with dual projection |
| `templates/中国地图示例(frykit).md` | China-wide temperature map with South China Sea inset |

## RS-Reasonix Integration

- Verify input data CRS with `mcp__geocode__read_geo_data` before mapping.
- Reproject with `mcp__geocode__run_qgis_algorithm` (gdal:warpreproject) if needed.
- Use `mcp__geocode__qgis_doc` to look up styling/rendering algorithms for pre-processing.
- GEE-derived rasters can be downloaded with `mcp__geocode__run_gee_script` + `download_image()`.
