# frykit Map Tools User Guide

> This document systematically introduces how to use frykit's built-in tools to help build thematic maps.

---

## Installation

```bash
# Install only the utility functions (without map data)
pip install frykit

# Include built-in Chinese administrative division data
pip install frykit[data]
```

---

## Global Configuration

frykit provides a global configuration system that affects plotting behavior. You can set it at the beginning of a script or temporarily switch settings using a context manager.

```python
import frykit

# Modify global configuration directly
frykit.config.fast_transform = False   # Disable fast projection; use more precise Cartopy projection
frykit.config.skip_outside = True      # Skip geometries outside the viewport (enabled by default)
frykit.config.strict_clip = True       # Enable strict clipping to prevent overflow

# Temporarily switch using a context manager
with frykit.config.context(fast_transform=False, strict_clip=True):
    # Code here will use the temporary configuration
    ...
# Original configuration is restored after exiting the context
```

**Configuration options:**

| Option           | Type                    | Default  | Description                                                                                  |
| ---------------- | ----------------------- | -------- | -------------------------------------------------------------------------------------------- |
| `data_source`    | `'amap'` / `'tianditu'` | `'amap'` | Built-in map data source                                                                     |
| `fast_transform` | bool                    | `True`   | Whether to use pyproj for direct coordinate transform (faster but occasionally inaccurate)   |
| `skip_outside`   | bool                    | `True`   | Whether to skip geometries outside the Axes viewport (speeds up local maps)                  |
| `strict_clip`    | bool                    | `False`  | Whether to use strict clipping (prevents overflow at non-rectangular boundaries, but slower) |

---

## 1. Drawing Arbitrary Geometries — add_geometries

This is the core low-level function of frykit's plotting capabilities. It can draw any Shapely geometry object onto a Matplotlib Axes.

```python
import matplotlib.pyplot as plt
import shapely
import frykit.plot as fplt

fig, ax = plt.subplots()

# Draw a circle
circle = shapely.Point(116.4, 39.9).buffer(2)
fplt.add_geometries(ax, circle, fc='lightblue', ec='navy', lw=1.5)

plt.show()
```

### 1.1 Usage on GeoAxes

When `ax` is a Cartopy `GeoAxes`, `add_geometries` automatically projects geometries from the source CRS (`crs` parameter) to the `ax.projection` CRS.

```python
import cartopy.crs as ccrs

fig = plt.figure(figsize=(8, 6))
ax = fig.add_subplot(projection=ccrs.LambertConformal(central_longitude=105))

# Read your own shapefile
from cartopy.io.shapereader import Reader
reader = Reader('my_shapefile.shp')
geometries = list(reader.geometries())
reader.close()

# crs defaults to PlateCarree (lon/lat coordinates)
fplt.add_geometries(ax, geometries, fc='none', ec='k', lw=0.25)
```

### 1.2 Reading and Drawing GeoJSON

```python
import json
import shapely.geometry as sgeom

with open('my_data.geojson') as f:
    geojson = json.load(f)

geometries = [sgeom.shape(feat['geometry']) for feat in geojson['features']]
fplt.add_geometries(ax, geometries, fc='none', ec='k', lw=0.5)
```

### 1.3 Color-Filling by Value

Use the `array`, `cmap`, and `norm` parameters to achieve area-based color filling (similar to a choropleth map).

```python
import numpy as np

# Suppose there are 5 polygons with corresponding values
values = np.array([10, 25, 18, 33, 7])
fplt.add_geometries(
    ax, geometries,
    array=values,
    cmap='YlOrRd',
    ec='k', lw=0.5,
)
```

### 1.4 Key Parameters

| Parameter        | Description                                                                                            |
| ---------------- | ------------------------------------------------------------------------------------------------------ |
| `ax`             | Target Axes (either a regular Axes or GeoAxes)                                                         |
| `geometries`     | One or a collection of Shapely geometry objects                                                        |
| `crs`            | Source CRS of the geometries. Must be None for regular Axes; defaults to PlateCarree for GeoAxes       |
| `fast_transform` | Whether to use pyproj fast projection. None follows global config                                      |
| `skip_outside`   | Whether to skip geometries outside the viewport. None follows global config                            |
| `**kwargs`       | Parameters passed to PathCollection, such as `fc`, `ec`, `lw`, `array`, `cmap`, `norm`, `zorder`, etc. |

### 1.5 Differences from Cartopy's Native Method

| Comparison          | Cartopy `GeoAxes.add_geometries` | frykit `add_geometries`                                  |
| ------------------- | -------------------------------- | -------------------------------------------------------- |
| Regular Axes        | Not supported                    | Supported                                                |
| Projection speed    | Slower (especially v0.23+)       | Fast (direct pyproj transform + skip outside geometries) |
| Color-fill by value | Requires manual handling         | Built-in `array`/`cmap`/`norm` support                   |
| Caching             | None                             | Multi-level caching (Path + projection results)          |

---

## 2. Polygon Clipping — clip_by_polygon

Clip Matplotlib plot objects with any Shapely polygon, showing only the content within the polygon. Supports `contourf`, `pcolormesh`, `imshow`, `quiver`, `scatter`, `Text`, and nearly all Artist types.

```python
import numpy as np

ax = plt.axes(projection=fplt.PLATE_CARREE)

# Draw a filled contour plot
lon = np.linspace(100, 130, 100)
lat = np.linspace(20, 50, 100)
lon2d, lat2d = np.meshgrid(lon, lat)
data = np.sin(np.radians(lon2d)) * np.cos(np.radians(lat2d))

cf = ax.contourf(lon2d, lat2d, data, levels=20, cmap='RdBu_r',
                 transform=fplt.PLATE_CARREE)

# Clip with a custom polygon
my_polygon = shapely.box(105, 25, 125, 45)
fplt.clip_by_polygon(cf, my_polygon)
```

### 2.1 Clipping with Multiple Polygons

When a collection of polygons is passed, they are automatically merged using `shapely.union_all` before clipping.

```python
polygon_a = shapely.Point(110, 30).buffer(5)
polygon_b = shapely.Point(120, 35).buffer(5)
fplt.clip_by_polygon(cf, [polygon_a, polygon_b])
```

### 2.2 Clipping Multiple Artists at Once

```python
cf = ax.contourf(...)
Q = ax.quiver(...)
fplt.clip_by_polygon([cf, Q], my_polygon)
```

### 2.3 Strict Clipping Mode

When the GeoAxes boundary is non-rectangular (e.g., Lambert projection), normal clipping may overflow. Enable `strict_clip` to fix this:

```python
fplt.clip_by_polygon(cf, my_polygon, strict_clip=True)

# Or enable globally
frykit.config.strict_clip = True
```

### 2.4 Manually Combining Polygons for Complex Clipping

```python
import shapely

province_a = ...  # A province polygon
city_b = ...      # A city polygon
city_c = ...      # Another city polygon

# Merge into a single polygon
combined = shapely.union_all([province_a, city_b, city_c])
fplt.clip_by_polygon(artist, combined)
```

---

## 3. Set Map Extent and Ticks in One Line — set_map_ticks

Setting map extent and ticks natively in Cartopy requires many lines of code. `set_map_ticks` does it all in one line.

### 3.1 Basic Usage

```python
ax = plt.axes(projection=fplt.PLATE_CARREE)

# Set extent with 10° longitude and 10° latitude intervals
fplt.set_map_ticks(ax, extents=(70, 140, 0, 60), dx=10, dy=10)
```

This single line is equivalent to:

```python
import cartopy.crs as ccrs
from cartopy.mpl.ticker import LongitudeFormatter, LatitudeFormatter

crs = ccrs.PlateCarree()
ax.set_extent((70, 140, 0, 60), crs=crs)
ax.set_xticks(np.arange(70, 141, 10), crs=crs)
ax.set_yticks(np.arange(0, 61, 10), crs=crs)
ax.xaxis.set_major_formatter(LongitudeFormatter())
ax.yaxis.set_major_formatter(LatitudeFormatter())
```

### 3.2 Adding Minor Ticks

```python
# mx=1 means inserting 1 minor tick between each pair of major ticks (i.e., 5° interval minor ticks)
fplt.set_map_ticks(ax, extents=(70, 140, 0, 60), dx=10, dy=10, mx=1, my=1)
```

### 3.3 Manually Specifying Ticks

```python
fplt.set_map_ticks(
    ax,
    extents=(100, 130, 20, 50),
    xticks=[100, 110, 120, 130],
    yticks=[20, 30, 40, 50],
)
```

### 3.4 Global Extent

```python
fplt.set_map_ticks(ax, extents='global', dx=30, dy=30)
```

### 3.5 Usage with Regular Axes

When `ax` is a regular Axes, its projection is assumed to be PlateCarree, and longitude/latitude formatted tick labels are added automatically.

```python
fig, ax = plt.subplots()
ax.set_aspect(1)
fplt.set_map_ticks(ax, extents=(70, 140, 0, 60), dx=10, dy=10)
```

### 3.6 Usage with Non-PlateCarree Projections

```python
ax = plt.axes(projection=ccrs.LambertConformal(central_longitude=105))
fplt.set_map_ticks(ax, extents=(70, 140, 15, 55), dx=10, dy=10)
```

> **Note:** For non-PlateCarree projections, if the display extent is non-rectangular or crosses the 180° meridian, incorrect results may occur. In such cases, consider using `GeoAxes.gridlines` instead.

### 3.7 Tick Style Configuration

> **Note:** After using `set_map_ticks` to set the map's lat/lon ticks, you still need to use Matplotlib's native `ax.tick_params()` to control tick styling.

```python
# First set the extent and tick positions with frykit
fplt.set_map_ticks(ax, extents=(70, 140, 0, 60), dx=10, dy=10, mx=1, my=1)

# Then adjust tick styles with matplotlib
ax.tick_params(
    which='major',        # Major ticks
    length=6,             # Tick length
    width=1.2,            # Tick width
    direction='in',       # Direction: 'in' inward, 'out' outward, 'inout' both
    labelsize=10,         # Label font size
    color='black',        # Tick color
    labelcolor='black',   # Label color
)
ax.tick_params(
    which='minor',        # Minor ticks
    length=3,
    width=0.8,
    direction='in',
)
```

### 3.8 Full Parameter List

| Parameter    | Description                                                   | Default            |
| ------------ | ------------------------------------------------------------- | ------------------ |
| `extents`    | `(lon0, lon1, lat0, lat1)` or `'global'`                      | `'global'`         |
| `xticks`     | Manually specify x-axis major ticks (longitude); overrides dx | None               |
| `yticks`     | Manually specify y-axis major ticks (latitude); overrides dy  | None               |
| `dx`         | Longitude major tick interval                                 | 10                 |
| `dy`         | Latitude major tick interval                                  | 10                 |
| `mx`         | Number of longitude minor ticks                               | 0                  |
| `my`         | Number of latitude minor ticks                                | 0                  |
| `xformatter` | X-axis tick label formatter                                   | LongitudeFormatter |
| `yformatter` | Y-axis tick label formatter                                   | LatitudeFormatter  |

---

## 4. Adding a North Arrow — add_compass

```python
ax = plt.axes(projection=ccrs.LambertConformal(central_longitude=105))
fplt.set_map_ticks(ax, extents=(70, 140, 15, 55), dx=10, dy=10)

# Add a north arrow at Axes coordinates (0.92, 0.92)
fplt.add_compass(ax, x=0.92, y=0.92, size=15)
```

### 4.1 Three Styles

```python
fplt.add_compass(ax, 0.15, 0.85, size=15, style='arrow')   # Arrow (default)
fplt.add_compass(ax, 0.50, 0.85, size=15, style='star')    # Star
fplt.add_compass(ax, 0.85, 0.85, size=15, style='circle')  # Circle with ring
```

- **arrow** — Classic black-and-white dual-color arrow, clean and elegant
- **star** — Four-directional star compass, suitable for more decorative maps
- **circle** — Arrow with a central ring, resembling a traditional compass rose

### 4.2 Automatic North Detection

On a GeoAxes, the north arrow automatically calculates the true north direction based on its position. In non-PlateCarree projections such as Lambert, the north angle varies at different positions, and the north arrow correctly reflects this.

You can also manually specify the azimuth angle (in degrees) using the `angle` parameter:

```python
fplt.add_compass(ax, 0.92, 0.92, size=15, angle=10)  # Manually set to 10° east of north
```

### 4.3 Customizing Appearance

```python
fplt.add_compass(
    ax, 0.92, 0.92, size=20,
    pc_kwargs=dict(linewidth=1.5, edgecolor='navy'),     # Arrow style
    text_kwargs=dict(fontsize=14, fontweight='bold'),     # "N" text style
)
```

### 4.4 Full Parameters

| Parameter     | Description                                     | Default   |
| ------------- | ----------------------------------------------- | --------- |
| `x`, `y`      | Position in Axes coordinate system (0–1)        | —         |
| `angle`       | Manually specify azimuth (degrees). None = auto | None      |
| `size`        | North arrow size (pt)                           | 20        |
| `style`       | `'arrow'`, `'star'`, `'circle'`                 | `'arrow'` |
| `pc_kwargs`   | Style parameters for the arrow's PathCollection | None      |
| `text_kwargs` | Style parameters for the "N" Text               | None      |

---

## 5. Adding a Scale Bar — add_scale_bar

```python
ax = plt.axes(projection=fplt.PLATE_CARREE)
fplt.set_map_ticks(ax, extents=(70, 140, 0, 60), dx=10, dy=10)

# Add a 1000 km scale bar at Axes coordinates (0.055, 0.035)
scale_bar = fplt.add_scale_bar(ax, x=0.055, y=0.035, length=1000)
scale_bar.set_xticks([0, 500, 1000])
```

> **Note:** You can also customize the scale bar's style on top of the defaults.

```python
# Traditional black-and-white alternating scale bar
    scale_bar = fplt.add_scale_bar(ax, x=0.055, y=0.035, length=200) # Place as close to the lower-right corner as possible

# Transparent background
scale_bar.set_facecolor("none")
scale_bar.patch.set_alpha(0)

# Set tick labels: 0, 100, 200km
scale_bar.set_xticks([0, 100, 200])
scale_bar.set_xticklabels(["0", "100", "200km"])

# Tick style: font size 8, no tick lines (length=0), labels at bottom
scale_bar.tick_params(axis="x", labelsize=8, length=0, pad=2,
                      labelbottom=True, labeltop=False)

# Remove y-axis
scale_bar.set_yticks([])
scale_bar.set_xlabel("")
scale_bar.xaxis.label.set_visible(False)

# Move text below the black-and-white bar
scale_bar.xaxis.set_ticks_position("bottom")

# Hide all border spines
for spine in scale_bar.spines.values():
    spine.set_visible(False)

fplt.add_frame(scale_bar)
```

### 5.1 How It Works

The scale bar takes a short horizontal segment at the center of the GeoAxes and uses pyproj's geodetic functions (`geod.inv`) to calculate the real-world geographic distance per pixel. This makes it accurate under any projection. For regular Axes, PlateCarree projection is assumed and the calculation is based on the center latitude.

### 5.2 Using Meters as the Unit

```python
scale_bar = fplt.add_scale_bar(ax, 0.3, 0.08, length=50000, units='m')
scale_bar.set_xticks([0, 25000, 50000])
```

### 5.3 With GMT-Style Frame

The scale bar object inherits from Axes, so you can add a GMT-style black-and-white checkerboard for a more professional look:

```python
scale_bar = fplt.add_scale_bar(ax, 0.36, 0.08, length=1000)
scale_bar.set_xticks([0, 250, 500, 750, 1000])
fplt.add_frame(scale_bar)  # GMT-style scale bar
```

### 5.4 Further Customization

Since the scale bar is essentially an `Axes` object, you can use all Matplotlib Axes methods to customize it:

```python
scale_bar.tick_params(labelsize=7)
scale_bar.set_xlabel('km', fontsize=8)
```

---

## 6. GMT-Style Frame — add_frame

Add a GMT (Generic Mapping Tools) style black-and-white alternating checkerboard frame to an Axes. The checkerboard divisions are determined by the current major and minor ticks — denser ticks produce more checkerboard segments.

```python
ax = plt.axes(projection=fplt.PLATE_CARREE)
fplt.set_map_ticks(ax, extents=(70, 140, 0, 60), dx=10, dy=10, mx=1, my=1)

fplt.add_frame(ax, width=5)  # width unit is pt
```

### 6.1 Custom Colors

```python
fplt.add_frame(ax, width=6, edgecolor='navy', facecolor=['navy', 'white'])
```

> **Note:** Currently only supports PlateCarree and Mercator projections on GeoAxes.

---

## 7. Adding an Inset Map — add_mini_axes

Automatically place a proportionally scaled-down child Axes in a corner of the main plot. Commonly used for South China Sea inset maps, overview maps, etc.

```python
ax = plt.axes(projection=fplt.PLATE_CARREE)
fplt.set_map_ticks(ax, extents=(100, 125, 20, 45), dx=5, dy=5)

# Add a child plot scaled to 0.4x in the lower-right corner, inheriting the main plot's projection
mini_ax = fplt.add_mini_axes(ax, shrink=0.4, loc='lower right')

# Perform any operations on the child plot
mini_ax.set_extent((105, 122, 2, 25), crs=fplt.PLATE_CARREE)
# All frykit plotting functions can also be used on the child plot
```

### 7.1 Parameters

| Parameter    | Description                                                                             | Default         |
| ------------ | --------------------------------------------------------------------------------------- | --------------- |
| `shrink`     | Scale factor; 1.0 means same height or width as the main plot                           | 0.4             |
| `aspect`     | Height-to-width ratio; default 1 (consistent with GeoAxes)                              | 1               |
| `loc`        | `'lower left'`, `'lower right'`, `'upper left'`, `'upper right'`                        | `'lower right'` |
| `projection` | Child plot projection. `'same'` inherits from the main plot; `None` means no projection | `'same'`        |

### 7.2 Smart Positioning

Unlike manually calling `fig.add_axes([x, y, w, h])`, `add_mini_axes` does not require manual position calculation — it dynamically computes the child plot's position on each render, automatically snapping to the main plot's corner. It stays correctly positioned even if the figure size or main plot position changes.

---

## 8. Adding a Side Axes — add_side_axes

Add a new Axes of equal height or width alongside an existing Axes. The most common use case is placing a colorbar.

```python
ax = plt.axes(projection=fplt.PLATE_CARREE)
cf = ax.contourf(...)

# Add a colorbar Axes to the right of ax
cax = fplt.add_side_axes(ax, width=0.02, pad=0.02, loc='right')
plt.colorbar(cf, cax=cax)
```

### 8.1 Usage with Multiple Subplots

When an array of Axes is passed, their combined bounding box is automatically used for positioning, enabling a single colorbar for all subplots.

```python
fig, axes = plt.subplots(2, 2, subplot_kw=dict(projection=fplt.PLATE_CARREE))

# ... plot on each ax ...

cax = fplt.add_side_axes(axes, width=0.02, pad=0.02, loc='right')
plt.colorbar(cf, cax=cax)
```

### 8.2 Four Directions

```python
fplt.add_side_axes(ax, width=0.02, pad=0.02, loc='right')    # Right (default)
fplt.add_side_axes(ax, width=0.02, pad=0.02, loc='left')     # Left
fplt.add_side_axes(ax, width=0.02, pad=0.02, loc='bottom')   # Bottom (horizontal colorbar)
fplt.add_side_axes(ax, width=0.02, pad=0.02, loc='top')      # Top
```

> **Tip:** The units for both `width` and `pad` are in the Figure coordinate system (0–1), where `width` is the width (or height) of the new Axes, and `pad` is the spacing between them.

---

## 9. Drawing a Box on the Map — add_box

Draw a lon/lat extent box on the map, commonly used to mark study areas or regions of interest.

```python
ax = plt.axes(projection=fplt.PLATE_CARREE)

# Draw a lon/lat box with red dashed lines
fplt.add_box(ax, extents=(105, 120, 25, 40), ec='red', lw=2, fc='none', ls='--')
```

---

## 10. Batch Text Annotation — add_texts

Draw a set of text annotations on an Axes at once, using a custom `TextCollection` class internally.

```python
lons = [116.4, 121.5, 113.3, 104.1]
lats = [39.9, 31.2, 23.1, 30.6]
names = ['Beijing', 'Shanghai', 'Guangzhou', 'Chengdu']

text_collection = fplt.add_texts(
    ax, lons, lats, names,
    fontsize=8,
    transform=fplt.PLATE_CARREE,
)
```

`TextCollection` supports the `skip_outside` parameter (follows global config by default). When text coordinates fall outside the Axes display range, the text is automatically hidden to avoid label pileup at the map edges.

### Tip: Using polylabel for Polygon Label Placement

When annotating polygon features (e.g., provinces, administrative regions), a simple way to place labels is to use the polygon centroid. The centroid is the geometric center of the polygon and is easy to compute with Shapely. For **MultiPolygon** geometries, you can use the centroid of the largest sub-polygon.

````python
from shapely.ops import polylabel

lons, lats, names = [], [], []
for record in province_records:
    geom = record.geometry
    # For MultiPolygon, use the largest sub-polygon
    if geom.geom_type == "MultiPolygon":
        geom = max(geom.geoms, key=lambda g: g.area)
    point = polylabel(geom, tolerance=0.01)
    lons.append(point.x)
    lats.append(point.y)
    names.append(record.attributes["name"])

fplt.add_texts(ax, lons, lats, names, fontsize=10, transform=fplt.PLATE_CARREE)


---

## 11. Color Tools

### 11.1 Zero-Centered Colorbar — CenteredBoundaryNorm

In meteorological plotting, it is common to use warm/cool colors for positive/negative anomalies with white near zero. `CenteredBoundaryNorm` automatically aligns the interval containing zero to the center of the colormap.

```python
boundaries = [-10, -5, -2, -1, 1, 2, 5, 10, 20, 50, 100]
norm = fplt.CenteredBoundaryNorm(boundaries)

# Use with a diverging colormap
cf = ax.contourf(lon, lat, anomaly_data,
                 levels=boundaries, cmap='RdBu_r', norm=norm)
````

Difference from standard `BoundaryNorm`: standard `BoundaryNorm` distributes color levels uniformly, causing the zero interval to shift toward one side; `CenteredBoundaryNorm` ensures the zero interval falls exactly at the colormap's midpoint (white).

### 11.2 Qualitative Palette — make_qualitative_palette

Construct a "one color per category" cmap + norm + ticks combination, suitable for categorical data.

```python
colors = ['orangered', 'orange', 'yellow', 'limegreen', 'royalblue', 'darkviolet']
cmap, norm, ticks = fplt.make_qualitative_palette(colors)

# Draw a colorbar preview
cbar = fplt.plot_colormap(cmap, norm)
cbar.set_ticks(ticks)
cbar.set_ticklabels(['Type A', 'Type B', 'Type C', 'Type D', 'Type E', 'Type F'])
```

### 11.3 Quick Colormap Preview — plot_colormap

Draw a standalone colorbar preview, useful for debugging color schemes.

```python
from matplotlib.colors import BoundaryNorm

boundaries = [-10, -5, -2, -1, 1, 2, 5, 10]
norm = fplt.CenteredBoundaryNorm(boundaries)
cbar = fplt.plot_colormap(plt.cm.RdBu_r, norm)
cbar.set_ticks(boundaries)
```

---

## 12. Subplot Labeling — letter_axes

Automatically add (a), (b), (c)... labels to a set of subplots.

```python
fig, axes = plt.subplots(2, 2)
fplt.letter_axes(axes)  # Auto-labels (a) (b) (c) (d)
```

---

## Appendix: frykit Map Tools Quick Reference

| Need                    | Function                   | One-Line Description                                    |
| ----------------------- | -------------------------- | ------------------------------------------------------- |
| Draw Shapely geometries | `add_geometries`           | Supports regular Axes and GeoAxes with auto projection  |
| Clip by polygon         | `clip_by_polygon`          | Clips contourf/quiver/scatter and any other Artist      |
| Set extent and ticks    | `set_map_ticks`            | One line for extent + ticks + formatter                 |
| North arrow             | `add_compass`              | Three styles, auto north on GeoAxes                     |
| Scale bar               | `add_scale_bar`            | Auto geographic distance calculation for any projection |
| GMT frame               | `add_frame`                | Generates black-and-white checkerboard frame from ticks |
| Inset map               | `add_mini_axes`            | Auto-positioned in corner, commonly for South China Sea |
| Side Axes               | `add_side_axes`            | Add equal-height/width Axes alongside for colorbar      |
| Draw box                | `add_box`                  | Mark a rectangular region on the map                    |
| Batch annotation        | `add_texts`                | Draw multiple text annotations at once                  |
| Zero-centered colorbar  | `CenteredBoundaryNorm`     | Ensures zero interval maps to colormap center           |
| Qualitative palette     | `make_qualitative_palette` | One color per category for categorical fills            |
| Preview colormap        | `plot_colormap`            | Quickly preview color scheme                            |
| Subplot labels          | `letter_axes`              | Auto-add (a)(b)(c) labels                               |
