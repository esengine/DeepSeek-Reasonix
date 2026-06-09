# Cartopy Basemap Configuration Complete Guide

> This document systematically introduces how to configure and customize basemaps when creating maps with Cartopy. It covers two main approaches: building basemaps with `cartopy.feature` built-in features, and using third-party online tile map services (OSM, ArcGIS, etc.) as basemaps.

---

## Table of Contents

1. [Overview: Two Basemap Approaches](#1-overview-two-basemap-approaches)
2. [Approach 1: Building Basemaps with cartopy.feature](#2-approach-1-building-basemaps-with-cartopyfeature)
   - 2.1 Module Overview
   - 2.2 Predefined Feature Constants
   - 2.3 NaturalEarthFeature Extended Features
   - 2.4 Other Feature Classes
   - 2.5 Resolution Control and Adaptive Scaling
   - 2.6 Style Control
3. [Approach 2: Using Third-Party Online Tile Maps as Basemaps](#3-approach-2-using-third-party-online-tile-maps-as-basemaps)
   - 3.1 Tile Map Fundamentals
   - 3.2 Loading XYZ Tiles with add_image()
   - 3.3 Loading WMTS Services with add_wmts()
   - 3.4 Automatic Zoom Level Selection
4. [Comparison and Hybrid Usage](#4-comparison-and-hybrid-usage)
5. [Reference Resources](#5-reference-resources)

---

## 1. Overview: Two Basemap Approaches

When creating maps with Cartopy, the basemap is the visual foundation of the entire map. Cartopy provides two complementary approaches for building basemaps:

**Approach 1: `cartopy.feature` built-in feature approach.** Build basemaps by layering coastlines, borders, land fills, rivers, lakes, and other vector features. Data comes from public datasets such as Natural Earth and GSHHS, works entirely offline, and allows full style customization. Suitable for academic papers, meteorological charts, data visualization, and other scenarios requiring custom basemap styling.

**Approach 2: Third-party online tile map approach.** Load raster basemaps directly from online tile map services such as OpenStreetMap, ArcGIS Online, and Google Maps via the `cartopy.io.img_tiles` module. These basemaps are visually polished, information-rich, and ready to use out of the box. Suitable for scenarios requiring street-level detail, satellite imagery, or terrain shading.

The two approaches are not mutually exclusive — they can be combined on the same map. For example, you can overlay vector borders on a satellite imagery basemap.

---

## 2. Approach 1: Building Basemaps with cartopy.feature

### 2.1 Module Overview

The `cartopy.feature` module provides a Feature Interface for adding various geographic features to GeoAxes maps. All features are added using the `ax.add_feature()` method.

The default draw order (zorder) for Cartopy features is 1.5, placing them above images and fills but below lines and text.

```python
import matplotlib.pyplot as plt
import cartopy.crs as ccrs
import cartopy.feature as cfeature

fig, ax = plt.subplots(subplot_kw={'projection': ccrs.PlateCarree()})
ax.add_feature(cfeature.COASTLINE)
ax.add_feature(cfeature.LAND, facecolor='lightyellow')
ax.add_feature(cfeature.OCEAN, facecolor='lightblue')
plt.show()
```

### 2.2 Predefined Feature Constants

Cartopy provides 7 predefined commonly used feature constants that can be used directly and support adaptive resolution scaling.

#### 2.2.1 COASTLINE

- **Dataset:** `physical / coastline`
- **Geometry type:** Line (LineString)
- **Description:** Global coastlines and major island outlines — the most basic and frequently used basemap feature.

```python
ax.add_feature(cfeature.COASTLINE, linewidth=0.5)
```

#### 2.2.2 BORDERS — National Boundaries

- **Dataset:** `cultural / admin_0_boundary_lines_land`
- **Geometry type:** Line (LineString)
- **Description:** Land boundaries between all sovereign nations. Natural Earth draws boundaries by de facto control by default.

```python
ax.add_feature(cfeature.BORDERS, linestyle=':', linewidth=0.8, edgecolor='gray')
```

#### 2.2.3 STATES — First-Level Administrative Divisions

- **Dataset:** `cultural / admin_1_states_provinces_lines`
- **Geometry type:** Line (LineString)
- **Description:** First-level administrative division boundaries within countries, such as US state lines or Chinese provincial boundaries.

```python
ax.add_feature(cfeature.STATES, linestyle='--', linewidth=0.5)
```

#### 2.2.4 LAND

- **Dataset:** `physical / land`
- **Geometry type:** Polygon
- **Description:** Global land polygons including major islands. Default fill color is Natural Earth's preset light yellow.

```python
ax.add_feature(cfeature.LAND, facecolor='lightgreen')
```

#### 2.2.5 OCEAN

- **Dataset:** `physical / ocean`
- **Geometry type:** Polygon
- **Description:** Global ocean polygons. Default fill color is light blue.

```python
ax.add_feature(cfeature.OCEAN, facecolor='lightblue')
```

#### 2.2.6 LAKES

- **Dataset:** `physical / lakes`
- **Geometry type:** Polygon
- **Description:** Global natural lakes and reservoirs.

```python
ax.add_feature(cfeature.LAKES, facecolor='lightblue', edgecolor='blue', alpha=0.5)
```

#### 2.2.7 RIVERS

- **Dataset:** `physical / rivers_lake_centerlines`
- **Geometry type:** Line (LineString)
- **Description:** Single-line drainage network including rivers and lake centerlines.

```python
ax.add_feature(cfeature.RIVERS, edgecolor='steelblue', linewidth=0.6)
```

#### Predefined Feature Summary

| Constant    | category | name                           | Geometry | Purpose                           |
| ----------- | -------- | ------------------------------ | -------- | --------------------------------- |
| `COASTLINE` | physical | coastline                      | Line     | Coastlines and major island outlines |
| `BORDERS`   | cultural | admin_0_boundary_lines_land    | Line     | National boundaries               |
| `STATES`    | cultural | admin_1_states_provinces_lines | Line     | First-level admin division lines  |
| `LAND`      | physical | land                           | Polygon  | Land fill                         |
| `OCEAN`     | physical | ocean                          | Polygon  | Ocean fill                        |
| `LAKES`     | physical | lakes                          | Polygon  | Lake fill                         |
| `RIVERS`    | physical | rivers_lake_centerlines        | Line     | Rivers                            |

### 2.3 NaturalEarthFeature Extended Features

The predefined constants cover only the most commonly used features. The `NaturalEarthFeature` class provides access to the full range of features in the Natural Earth dataset.

```python
cfeature.NaturalEarthFeature(category, name, scale, **kwargs)
```

Parameters: `category` is `'physical'` or `'cultural'`; `name` is the dataset name; `scale` is `'10m'`, `'50m'`, or `'110m'`.

#### 2.3.1 Physical Features (Physical Geography)

| name                          | Description                           | Available Scales |
| ----------------------------- | ------------------------------------- | ---------------- |
| `coastline`                   | Coastlines                            | 10m / 50m / 110m |
| `land`                        | Land polygons                         | 10m / 50m / 110m |
| `ocean`                       | Ocean polygons                        | 10m / 50m / 110m |
| `lakes`                       | Lakes                                 | 10m / 50m / 110m |
| `rivers_lake_centerlines`     | Rivers and lake centerlines           | 10m / 50m / 110m |
| `minor_islands`               | Minor islands                         | 10m              |
| `reefs`                       | Coral reefs                           | 10m              |
| `glaciated_areas`             | Glaciers and recently deglaciated areas | 10m / 50m / 110m |
| `antarctic_ice_shelves_polys` | Antarctic ice shelf polygons          | 10m / 50m        |
| `bathymetry_all`              | Bathymetric contours                  | 10m              |
| `geographic_lines`            | Equator, tropics, date line, etc.     | 10m / 50m / 110m |
| `geography_marine_polys`      | Marine region polygons                | 10m / 50m / 110m |
| `geography_regions_polys`     | Geographic region polygons            | 10m / 50m / 110m |
| `playas`                      | Salt flats and intermittent lakes     | 10m / 50m        |
| `graticules_all`              | Graticules (various intervals)        | 10m              |

```python
# Glaciated areas
glaciers = cfeature.NaturalEarthFeature(
    'physical', 'glaciated_areas', '50m',
    facecolor='lightcyan', edgecolor='steelblue'
)
ax.add_feature(glaciers)

# Geographic reference lines
geo_lines = cfeature.NaturalEarthFeature(
    'physical', 'geographic_lines', '110m',
    edgecolor='gray', facecolor='none', linestyle='--'
)
ax.add_feature(geo_lines)
```

#### 2.3.2 Cultural Features (Human Geography)

| name                               | Description                      | Available Scales |
| ---------------------------------- | -------------------------------- | ---------------- |
| `admin_0_countries`                | Sovereign country polygons       | 10m / 50m / 110m |
| `admin_0_boundary_lines_land`      | National boundaries (land)       | 10m / 50m / 110m |
| `admin_1_states_provinces`         | First-level admin division polygons | 10m / 50m     |
| `admin_1_states_provinces_lines`   | First-level admin division lines | 10m / 50m / 110m |
| `populated_places`                 | Cities / populated places        | 10m / 50m / 110m |
| `urban_areas`                      | Urbanized areas                  | 10m / 50m        |
| `roads`                            | Roads                            | 10m              |
| `railroads`                        | Railroads                        | 10m              |
| `airports`                         | Airports                         | 10m              |
| `ports`                            | Ports                            | 10m              |
| `time_zones`                       | Time zones                       | 10m              |
| `parks_and_protected_lands`        | Parks and protected areas        | 10m              |
| `admin_0_breakaway_disputed_areas` | Breakaway / disputed territories | 10m / 50m        |

```python
# Provincial administrative divisions
provinces = cfeature.NaturalEarthFeature(
    'cultural', 'admin_1_states_provinces_lines', '50m',
    edgecolor='gray', facecolor='none', linestyle=':'
)
ax.add_feature(provinces)

# Urbanized areas
urban = cfeature.NaturalEarthFeature(
    'cultural', 'urban_areas', '50m',
    edgecolor='none', facecolor='salmon', alpha=0.5
)
ax.add_feature(urban)
```

### 2.4 Other Feature Classes

In addition to NaturalEarthFeature, `cartopy.feature` provides the following feature classes:

**ShapelyFeature** — Draws arbitrary Shapely geometry objects on the map, suitable for loading custom shapefiles or manually constructed geometric regions.

```python
from shapely.geometry import Polygon

polygon = Polygon([(-75, 40), (-75, 45), (-70, 45), (-70, 40)])
feature = cfeature.ShapelyFeature(
    [polygon], ccrs.PlateCarree(),
    facecolor='red', alpha=0.3, edgecolor='darkred'
)
ax.add_feature(feature)
```

**GSHHSFeature** — Interface to the GSHHS (Global Self-consistent, Hierarchical, High-resolution Geography Database) coastline dataset, independent of Natural Earth, providing multi-level resolution data from coarse to full. `scale` options: `'auto'`, `'coarse'`, `'low'`, `'intermediate'`, `'high'`, `'full'`; `levels` parameter controls feature levels: 1 = coastline, 2 = lakeshore, 3 = island-in-lake, 4 = pond-on-island-in-lake.

```python
gshhs = cfeature.GSHHSFeature(scale='intermediate', levels=[1, 2])
ax.add_feature(gshhs, edgecolor='navy', facecolor='none')
```

**WFSFeature** — Retrieves geometry data from an OGC Web Feature Service (WFS) and renders it. Requires the `owslib` library.

**Nightshade** — `cartopy.feature.nightshade.Nightshade` draws the dark side of Earth (nighttime area) on the map, accounting for atmospheric refraction.

```python
from cartopy.feature.nightshade import Nightshade
from datetime import datetime

ax.add_feature(Nightshade(datetime.utcnow(), alpha=0.2))
```

### 2.5 Resolution Control and Adaptive Scaling

#### Three Resolution Levels

Natural Earth provides data at three scales:

| Resolution | Scale         | Use Case                    |
| ---------- | ------------- | --------------------------- |
| `'110m'`   | 1:110,000,000 | Global views, overview maps |
| `'50m'`    | 1:50,000,000  | Continental / regional maps |
| `'10m'`    | 1:10,000,000  | Country / province-level detail |

#### with_scale() Quick Switch

Predefined constants can quickly switch resolution using `with_scale()` without manually writing `NaturalEarthFeature`:

```python
ax.add_feature(cfeature.COASTLINE.with_scale('10m'))
ax.add_feature(cfeature.BORDERS.with_scale('50m'))
ax.add_feature(cfeature.LAKES.with_scale('10m'))
```

#### AdaptiveScaler

Starting from Cartopy 0.22, predefined features use `AdaptiveScaler` to automatically select the appropriate resolution based on the map's display extent. Coarser resolution is used for larger extents, and higher resolution is automatically applied when zooming in to smaller areas.

```python
from cartopy.feature import AdaptiveScaler

scaler = AdaptiveScaler(default_scale='110m', limits=(
    ('50m', 50),   # Switch to 50m when view extent < 50°
    ('10m', 15),   # Switch to 10m when view extent < 15°
))
```

### 2.6 Style Control

All features accept standard Matplotlib keyword arguments for appearance control when added:

| Parameter   | Description    | Example                    |
| ----------- | -------------- | -------------------------- |
| `edgecolor` | Edge color     | `'black'`, `'#2E75B6'`    |
| `facecolor` | Fill color     | `'lightgreen'`, `'none'`  |
| `linewidth` | Line width     | `0.5`, `1.0`              |
| `linestyle` | Line style     | `'-'`, `'--'`, `':'`      |
| `alpha`     | Transparency   | `0.3`, `0.7`              |
| `zorder`    | Draw order     | Default `1.5`             |

`cartopy.feature.COLORS` also provides a set of predefined color schemes: `'land'` (light yellow), `'land_alt1'` (light gray), `'water'` (light blue).

```python
ax.add_feature(cfeature.LAND, facecolor=cfeature.COLORS['land'])
ax.add_feature(cfeature.OCEAN, facecolor=cfeature.COLORS['water'])
```

---

## 3. Approach 2: Using Third-Party Online Tile Maps as Basemaps

### 3.1 Tile Map Fundamentals

The core idea of online tile maps is to slice Earth's surface into numerous fixed-size tiles (typically 256×256 pixels) at different zoom levels. Each tile is uniquely identified by three coordinates `(z, x, y)`, where `z` is the zoom level, and `x` and `y` are the column and row numbers at that level.

Each zoom level increase doubles the linear resolution, quadrupling the total number of tiles. At zoom=0, the entire globe is covered by 1 tile; at zoom=1, 4 tiles; at zoom=n, 4^n tiles.

Cartopy provides online tile map support through the `cartopy.io.img_tiles` module, with the core method being `ax.add_image()`.

### 3.2 Loading XYZ Tiles with add_image()

#### 3.2.1 Basic Usage

```python
import cartopy.crs as ccrs
import cartopy.io.img_tiles as cimgt
import matplotlib.pyplot as plt

tiles = cimgt.OSM()  # Create a tile object
fig, ax = plt.subplots(subplot_kw={'projection': tiles.crs})
ax.set_extent([100, 125, 20, 45])
ax.add_image(tiles, 6)  # Second parameter is the zoom level
plt.show()
```

Note: When creating axes, it is recommended to use `tiles.crs` as the projection to ensure tiles render correctly.

#### 3.2.2 OpenStreetMap Basemap

OSM is the most commonly used free tile map source. Cartopy includes a built-in `OSM` class for direct use:

```python
osm_tiles = cimgt.OSM()

fig, ax = plt.subplots(subplot_kw={'projection': osm_tiles.crs})
ax.set_extent([115.5, 117.5, 39.4, 41.1])  # Beijing area
ax.add_image(osm_tiles, 10)
```

#### 3.2.3 ArcGIS Basemaps

ArcGIS Online provides several high-quality tile map services. Cartopy does not have a dedicated ArcGIS class, but the `GoogleTiles` class supports specifying any tile source via the `url` parameter:

```python
# ArcGIS World Street Map
arcgis_street = cimgt.GoogleTiles(
    url='https://server.arcgisonline.com/ArcGIS/rest/services/'
        'World_Street_Map/MapServer/tile/{z}/{y}/{x}.jpg'
)

# ArcGIS World Imagery (Satellite)
arcgis_imagery = cimgt.GoogleTiles(
    url='https://server.arcgisonline.com/ArcGIS/rest/services/'
        'World_Imagery/MapServer/tile/{z}/{y}/{x}.jpg'
)

# ArcGIS World Topographic Map
arcgis_topo = cimgt.GoogleTiles(
    url='https://server.arcgisonline.com/ArcGIS/rest/services/'
        'World_Topo_Map/MapServer/tile/{z}/{y}/{x}.jpg'
)

# ArcGIS Shaded Relief
arcgis_relief = cimgt.GoogleTiles(
    url='https://server.arcgisonline.com/ArcGIS/rest/services/'
        'World_Shaded_Relief/MapServer/tile/{z}/{y}/{x}.jpg'
)

fig, ax = plt.subplots(subplot_kw={'projection': arcgis_imagery.crs})
ax.set_extent([100, 125, 20, 45])
ax.add_image(arcgis_imagery, 6)
```

#### 3.2.4 Other Built-in Tile Sources

| Class              | Description                    | Notes                                                                      |
| ------------------ | ------------------------------ | -------------------------------------------------------------------------- |
| `OSM`              | OpenStreetMap street map       | Free, must comply with usage policy                                        |
| `GoogleTiles`      | Google Maps                    | Supports `style` parameter: `'street'`/`'satellite'`/`'terrain'`/`'only_streets'` |
| `Stamen`           | Stamen stylized maps           | Supports `'terrain'`/`'toner'`/`'watercolor'` and other artistic styles    |
| `MapboxTiles`      | Mapbox custom maps             | Requires API Key                                                           |
| `MapboxStyleTiles` | Mapbox custom styles           | Requires API Key                                                           |
| `OrdnanceSurvey`   | British Ordnance Survey maps   | Requires API Key, supports `'Outdoor'`/`'Road'`/`'Light'`/`'Night'`/`'Leisure'` |
| `QuadtreeTiles`    | Microsoft quadtree tiles       | Low-level abstract class                                                   |

#### 3.2.5 Zoom Level Selection Reference

| Zoom Level | Approximate Scale | Use Case              |
| ---------- | ----------------- | --------------------- |
| 1 - 3      | Global/continental | World map overview    |
| 4 - 6      | Large country/region | Country-level maps  |
| 7 - 9      | Province/state     | Province-level maps   |
| 10 - 12    | City               | City-level maps       |
| 13 - 15    | Neighborhood       | Street-level maps     |
| 16 - 18    | Building           | Building-level detail |

**Note:** Higher zoom levels exponentially increase the number of tiles to download, significantly slowing rendering. Choose the lowest acceptable zoom level based on actual needs.

#### 3.2.6 Enabling Caching

Avoid repeated tile downloads and improve efficiency:

```python
# Use cartopy's default cache directory
tiles = cimgt.OSM(cache=True)

# Specify a custom cache path
tiles = cimgt.OSM(cache='/path/to/tile_cache/')
```

### 3.3 Loading WMTS Services with add_wmts()

#### 3.3.1 What is WMTS

WMTS (Web Map Tile Service) is a standardized tile map service protocol defined by the OGC. The key difference from regular XYZ tile maps is that WMTS fully describes all server-side metadata through a **GetCapabilities** document, including:

- **TileMatrixSet**: Defines the complete tile matrix set, with each TileMatrix corresponding to a zoom level
- **ScaleDenominator**: The precise scale denominator for each level
- **TileWidth / TileHeight**: Tile pixel dimensions
- **MatrixWidth / MatrixHeight**: Grid row and column counts at each level
- **SupportedCRS**: Supported coordinate reference systems

The OGC specification assumes a pixel physical size of 0.28mm, establishing a deterministic relationship between scale and ground resolution: `Ground resolution (m/px) = ScaleDenominator × 0.00028`.

It is precisely this self-describing metadata that enables clients to automatically select the most appropriate zoom level.

#### 3.3.2 Basic Usage

```python
import cartopy.crs as ccrs
import matplotlib.pyplot as plt

ax = plt.axes(projection=ccrs.PlateCarree())

url = ('https://services.arcgisonline.com/arcgis/rest/services/'
       'World_Imagery/MapServer/WMTS/1.0.0/WMTSCapabilities.xml')
ax.add_wmts(url, 'World_Imagery')

ax.set_extent([100, 125, 20, 45])
plt.show()
```

`add_wmts()` requires the `owslib` library (`pip install owslib`).

#### 3.3.3 Common ArcGIS WMTS Service URLs

```python
base = 'https://services.arcgisonline.com/arcgis/rest/services'

services = {
    'World Imagery':    f'{base}/World_Imagery/MapServer/WMTS/1.0.0/WMTSCapabilities.xml',
    'World Street Map': f'{base}/World_Street_Map/MapServer/WMTS/1.0.0/WMTSCapabilities.xml',
    'World Topo Map':   f'{base}/World_Topo_Map/MapServer/WMTS/1.0.0/WMTSCapabilities.xml',
    'Shaded Relief':    f'{base}/World_Shaded_Relief/MapServer/WMTS/1.0.0/WMTSCapabilities.xml',
    'Light Gray Base':  f'{base}/Canvas/World_Light_Gray_Base/MapServer/WMTS/1.0.0/WMTSCapabilities.xml',
}
```

The second parameter of `add_wmts()` is the layer name, which typically matches the service name in the URL path, such as `'World_Imagery'`, `'World_Street_Map'`, etc.

#### 3.3.4 NASA WMTS Earth at Night Example

```python
url = 'https://map1c.vis.earthdata.nasa.gov/wmts-geo/wmts.cgi'
layer = 'VIIRS_CityLights_2012'

ax = plt.axes(projection=ccrs.PlateCarree())
ax.add_wmts(url, layer)
ax.set_extent((-15, 25, 35, 60))
plt.title('Suomi NPP Earth at Night')
plt.show()
```

### 3.4 Automatic Zoom Level Selection

#### 3.4.1 add_wmts() — Native Auto-Scaling

`add_wmts()` is the only natively supported auto-scaling tile loading method in Cartopy. It works as follows:

1. Requests the GetCapabilities document from the server to obtain the complete TileMatrixSet definition
2. Calculates the required ground resolution based on the current axes' display extent and figure pixel size (figsize × dpi)
3. Finds the TileMatrix level in the TileMatrixSet that best matches that pixel density
4. Requests and composites tiles at the corresponding level

This allows users to skip manually specifying zoom levels — the system automatically makes the optimal choice based on the view extent. Additionally, `add_wmts()` supports dynamically updating resolution during interactive pan and zoom.

#### 3.4.2 add_image() — Manual or Assisted Calculation

`add_image()` does not natively support auto-zoom. For regular XYZ tiles, you can write a helper function to automatically estimate the appropriate level based on the extent:

```python
import math

def auto_zoom(extent, figsize=(10, 8), dpi=100):
    """
    Automatically calculate an appropriate zoom level based on
    lon/lat extent and figure size.

    Parameters
    ----------
    extent : list
        [lon_min, lon_max, lat_min, lat_max] geographic extent
    figsize : tuple
        matplotlib figure size (width, height) in inches
    dpi : int
        Figure resolution

    Returns
    -------
    int
        Recommended zoom level
    """
    lon_min, lon_max, lat_min, lat_max = extent
    lon_range = lon_max - lon_min
    img_width_px = figsize[0] * dpi
    # At zoom=0, the entire world (360°) = 256px
    zoom = math.log2(img_width_px * 360 / (lon_range * 256))
    return max(1, min(int(zoom), 18))

# Usage example
extent = [115.5, 117.5, 39.4, 41.1]
zoom = auto_zoom(extent)  # Automatically yields ~9
ax.add_image(osm_tiles, zoom)
```

#### 3.4.3 Comparison of the Two Methods

| Feature          | add_image()             | add_wmts()                             |
| ---------------- | ----------------------- | -------------------------------------- |
| Zoom selection   | Manual                  | Automatic                              |
| Protocol         | Non-standard XYZ convention | OGC WMTS standard                   |
| Self-description | None                    | Full description via GetCapabilities   |
| Interactive zoom | Not supported           | Supported                              |
| Extra dependency | None                    | Requires `owslib`                      |
| Tile source variety | Very high (any XYZ source) | Limited to WMTS services           |
| Cache support    | `cache=True`            | Built-in caching                       |

---

## 4. Comparison and Hybrid Usage

### 4.1 Approach Comparison

| Dimension      | Feature Approach                            | Online Tile Approach                      |
| -------------- | ------------------------------------------- | ----------------------------------------- |
| Data type      | Vector                                      | Raster                                    |
| Network        | Downloads on first use, then works offline  | Requires internet for each render         |
| Style customization | Fully customizable                     | Determined by tile provider               |
| Information density | Selectively add needed features          | Fixed content (streets, landmarks, etc.)  |
| Zoom adaptation | via with_scale() or AdaptiveScaler         | Zoom level control                        |
| Use cases      | Academic papers, data viz, weather maps     | Navigation, urban planning, satellite imagery |
| File size      | Lightweight vector data                     | Large raster tiles                        |

### 4.2 Hybrid Usage

The two approaches can be flexibly combined on the same map. Typical scenarios include overlaying national boundaries on satellite imagery basemaps, or overlaying custom vector regions on OSM basemaps.

```python
import matplotlib.pyplot as plt
import cartopy.crs as ccrs
import cartopy.feature as cfeature
import cartopy.io.img_tiles as cimgt

# Create an ArcGIS satellite imagery basemap
imagery = cimgt.GoogleTiles(
    url='https://server.arcgisonline.com/ArcGIS/rest/services/'
        'World_Imagery/MapServer/tile/{z}/{y}/{x}.jpg'
)

fig, ax = plt.subplots(figsize=(12, 8),
                       subplot_kw={'projection': imagery.crs})
ax.set_extent([100, 125, 20, 45])

# Load raster basemap first
ax.add_image(imagery, 6)

# Then overlay vector features
ax.add_feature(cfeature.BORDERS, linewidth=1.2, edgecolor='yellow')
ax.add_feature(cfeature.COASTLINE.with_scale('50m'),
               linewidth=0.8, edgecolor='white')

plt.title('Satellite Imagery + Vector Borders')
plt.show()
```

**Note on layering order:** Add the raster basemap (`add_image` / `add_wmts`) first, then overlay vector features (`add_feature`). Otherwise, vector features will be obscured by the raster basemap. If needed, you can also explicitly control the draw order via the `zorder` parameter.

---

## 5. Reference Resources

**Cartopy Official Documentation**

- Homepage: https://scitools.org.uk/cartopy/docs/latest/
- Feature module API: https://scitools.org.uk/cartopy/docs/latest/reference/feature.html
- img_tiles module API: https://scitools.org.uk/cartopy/docs/latest/reference/io.html
- Example gallery: https://scitools.org.uk/cartopy/docs/latest/gallery/index.html

**Data Sources**

- Natural Earth: https://www.naturalearthdata.com/
- GSHHS: https://www.ngdc.noaa.gov/mgg/shorelines/gshhs.html
- ArcGIS Online Services: https://server.arcgisonline.com/arcgis/rest/services/
- OpenStreetMap: https://www.openstreetmap.org/

**OGC Standards**

- WMTS Standard: https://www.ogc.org/standards/wmts/
- 2D Tile Matrix Set Standard: https://www.ogc.org/standards/tms/
