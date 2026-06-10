# Cartopy `ccrs` Built-in Projections Complete Guide

> Based on the latest version of Cartopy (v0.25), this document systematically introduces all built-in base classes, utility functions, and map projection classes in the `cartopy.crs` module.

## Table of Contents

- [Overview](#overview)
- [Basic Usage](#basic-usage)
- [Core Base Classes and Utility Functions](#core-base-classes-and-utility-functions)
- [I. Cylindrical Projections](#i-cylindrical-projections)
- [II. Conic Projections](#ii-conic-projections)
- [III. Azimuthal Projections](#iii-azimuthal-projections)
- [IV. Pseudocylindrical Projections](#iv-pseudocylindrical-projections)
- [V. Perspective Projections](#v-perspective-projections)
- [VI. Special-purpose Projections](#vi-special-purpose-projections)
- [VII. Polar Projections](#vii-polar-projections)
- [VIII. Regional Projections](#viii-regional-projections)
- [IX. Common Projections for China Maps](#ix-common-projections-for-china-maps)
- [Projection Classification Quick Reference](#projection-classification-quick-reference)
- [References](#references)

---

## Overview

`cartopy.crs` (CRS = Coordinate Reference Systems) is the core module of Cartopy, providing **37** built-in map projections, along with base classes such as `CRS`, `Globe`, `Geodetic`, and `Geocentric`, and the `epsg()` query function. These projections serve two main purposes:

1. **As canvas projection** (`projection` parameter): determines how the map is rendered
2. **As data coordinate system** (`transform` parameter): declares the coordinate system of the input data

All projection classes inherit from the `cartopy.crs.CRS` base class.

---

## Basic Usage

```python
import cartopy.crs as ccrs
import matplotlib.pyplot as plt

# Create a projection instance
projection = ccrs.Robinson(central_longitude=105)

# Use as canvas projection
fig, ax = plt.subplots(subplot_kw={'projection': projection})
ax.coastlines()

# Data coordinate transform: tell Cartopy the data uses lon/lat coordinates
ax.plot(116.4, 39.9, 'ro', transform=ccrs.PlateCarree())  # Beijing

plt.show()
```

---

## Core Base Classes and Utility Functions

In addition to specific projection classes, the `cartopy.crs` module provides a set of base classes and utility functions that form the foundation of the entire coordinate reference system.

### CRS — Coordinate Reference System Base Class

```python
cartopy.crs.CRS(proj4_params, globe=None)
```

`CRS` is the **root base class for all coordinate reference systems** in Cartopy. All projections and coordinate systems inherit from it. It defines coordinate reference systems through the Proj library and provides core methods such as coordinate transformation (`transform_point`, `transform_points`, `transform_vectors`), obtaining geodetic coordinates (`as_geodetic()`), and geocentric coordinates (`as_geocentric()`).

You typically do not need to instantiate `CRS` directly — use its subclasses instead.

---

### Globe — Ellipsoid Definition

```python
cartopy.crs.Globe(
    datum=None,
    ellipse='WGS84',
    semimajor_axis=None,
    semiminor_axis=None,
    flattening=None,
    inverse_flattening=None,
    towgs84=None,
    nadgrids=None
)
```

`Globe` defines the Earth ellipsoid model and its correspondence to the real world. All CRS instances are associated with a Globe instance, defaulting to the WGS84 reference ellipsoid. You can customize Globe to simulate other planets or use different geodetic datums.

```python
# Example: Create a projection based on the Mars ellipsoid
mars_globe = ccrs.Globe(semimajor_axis=3396190, semiminor_axis=3376200, ellipse=None)
mars_proj = ccrs.Orthographic(central_longitude=0, central_latitude=0, globe=mars_globe)
```

---

### Geodetic — Geodetic Coordinate System

```python
cartopy.crs.Geodetic(globe=None)
```

Defines a lon/lat coordinate system (spherical topology, geographic distance) with coordinates in degrees. It is a 3D coordinate system that cannot be used directly for map rendering (cannot be used as a `projection` parameter), but can be used as a `transform` parameter to declare that data uses geodetic coordinates.

**Difference from `PlateCarree`:** `Geodetic` performs great circle (geodesic) interpolation, while `PlateCarree` performs straight-line interpolation. When drawing long-distance line segments, `transform=ccrs.Geodetic()` draws great circle arcs, while `transform=ccrs.PlateCarree()` draws straight lines.

```python
# Draw a great circle route from Beijing to New York using Geodetic
ax.plot([116.4, -74.0], [39.9, 40.7], transform=ccrs.Geodetic(), color='red')
```

---

### Geocentric — Geocentric Coordinate System

```python
cartopy.crs.Geocentric(globe=None)
```

Defines a geocentric Cartesian coordinate system with its origin at Earth's center, using x/y/z Cartesian coordinates (units: meters). Primarily used as an intermediate step in 3D coordinate transformations; typically not used directly.

---

### RotatedGeodetic — Rotated Geodetic Coordinate System

```python
cartopy.crs.RotatedGeodetic(
    pole_longitude,
    pole_latitude,
    central_rotated_longitude=0.0,
    globe=None
)
```

A geodetic coordinate system with a rotated pole (spherical topology, geographic distance), with coordinates in degrees. Similar to the `RotatedPole` projection, but `RotatedGeodetic` is a 3D coordinate system that cannot be used directly for map rendering. Typically used for coordinate transformations in climate model data.

---

### ccrs.epsg() — EPSG Projection Query Function

```python
cartopy.crs.epsg(code)
```

**This is an extremely frequently used function in practice.** Returns the corresponding Cartopy projection object for a given EPSG code, allowing you to directly use thousands of standard coordinate systems worldwide without manually configuring projection parameters.

**Key limitations:**

- The EPSG code must correspond to a **Projected CRS**, such as 3857 (Web Mercator) or 32650 (UTM Zone 50N)
- **Does not support** geographic CRS EPSG codes, such as 4326 (WGS-84 lon/lat)
- Newer versions (v0.22+) perform conversion via `pyproj.CRS`; earlier versions required an online query to epsg.io

```python
# Use EPSG:3857 (Web Mercator) projection
web_mercator = ccrs.epsg(3857)
fig, ax = plt.subplots(subplot_kw={'projection': web_mercator})
ax.coastlines()

# Use EPSG:32650 (UTM Zone 50N)
utm50n = ccrs.epsg(32650)

# Use EPSG:2154 (France Lambert-93 official CRS)
lambert93 = ccrs.epsg(2154)
```

**Common EPSG codes quick reference:**

| EPSG Code   | Name                        | Description                              |
| ----------- | --------------------------- | ---------------------------------------- |
| 3857        | Web Mercator                | Google Maps / OpenStreetMap tile CRS     |
| 3395        | World Mercator              | WGS84 Mercator                           |
| 32601–32660 | UTM Zones 1N–60N            | Northern hemisphere UTM zones            |
| 32701–32760 | UTM Zones 1S–60S            | Southern hemisphere UTM zones            |
| 2154        | RGF93 / Lambert-93          | France official CRS                      |
| 27700       | OSGB 1936                   | British National Grid                    |
| 3035        | ETRS89-LAEA                 | European equal-area projection           |
| 2326        | Hong Kong 1980 Grid         | Hong Kong CRS                            |
| 4547        | CGCS2000 / 3-degree Zone 39 | China CGCS2000 CRS (3° zone)             |

> **Tip:** You can look up all EPSG codes and their details at https://epsg.io/.

---

## I. Cylindrical Projections

### 1. PlateCarree — Equidistant Cylindrical Projection

```python
ccrs.PlateCarree(central_longitude=0.0, globe=None)
```

The simplest and most commonly used projection, mapping lon/lat directly to Cartesian coordinates (x = longitude, y = latitude). Both meridians and parallels are equally spaced parallel straight lines. Often used as the default choice for the `transform` parameter.

**Use cases:** Global overview maps, reference CRS for data coordinate transformation.

---

### 2. Mercator — Mercator Projection

```python
ccrs.Mercator(
    central_longitude=0.0,
    min_latitude=-80.0,
    max_latitude=84.0,
    globe=None,
    latitude_true_scale=None,
    false_easting=0.0,
    false_northing=0.0,
    scale_factor=None
)
```

A conformal cylindrical projection that preserves local shapes but severely inflates areas at higher latitudes. Cannot represent areas near the poles (default latitude range -80° to 84°).

**Note:** `latitude_true_scale` and `scale_factor` are mutually exclusive — only one can be specified.

**Use cases:** Nautical charts, web map tiles (e.g., Google Maps base maps).

---

### 3. Miller — Miller Cylindrical Projection

```python
ccrs.Miller(central_longitude=0.0)
```

A modified Mercator projection that reduces area inflation at high latitudes, but is no longer conformal. It is a visual compromise between Mercator and Equidistant Cylindrical.

**Use cases:** Displaying the entire globe while keeping high-latitude distortion manageable.

---

### 4. LambertCylindrical — Lambert Equal-Area Cylindrical Projection

```python
ccrs.LambertCylindrical(central_longitude=0.0)
```

An equal-area cylindrical projection. Meridians are equally spaced, and parallels become closer together with increasing latitude to preserve area.

**Use cases:** Global maps requiring accurate area representation.

---

### 5. TransverseMercator — Transverse Mercator Projection

```python
ccrs.TransverseMercator(
    central_longitude=0.0,
    central_latitude=0.0,
    false_easting=0.0,
    false_northing=0.0,
    scale_factor=1.0,
    globe=None,
    approx=False
)
```

Rotates the Mercator cylinder 90° so that it is tangent to a meridian. Distortion is minimal near the central meridian.

**Use cases:** North-south oriented narrow strip regional maps, foundation of UTM projections.

---

### 6. UTM — Universal Transverse Mercator Projection

```python
ccrs.UTM(zone, southern_hemisphere=False, globe=None)
```

Divides the globe into 60 longitudinal zones (each 6° wide), each using an independent Transverse Mercator projection. `zone` ranges from 1 to 60.

**Use cases:** Military mapping, engineering surveying, large-scale topographic maps.

---

### 7. ObliqueMercator — Oblique Mercator Projection

```python
ccrs.ObliqueMercator(
    central_longitude=0.0,
    central_latitude=0.0,
    false_easting=0.0,
    false_northing=0.0,
    scale_factor=1.0,
    azimuth=0.0,
    globe=None
)
```

A Mercator projection with the cylinder axis tilted at an arbitrary angle, suitable for mapping along oblique strip-shaped regions.

**Use cases:** Regions extending in a specific direction (e.g., railway lines, coastlines, river courses).

> **Note:** This projection was introduced in Cartopy v0.20+.

---

## II. Conic Projections

### 8. AlbersEqualArea — Albers Equal-Area Conic Projection

```python
ccrs.AlbersEqualArea(
    central_longitude=0.0,
    central_latitude=0.0,
    false_easting=0.0,
    false_northing=0.0,
    standard_parallels=(20.0, 50.0),
    globe=None
)
```

An equal-area conic projection with no distortion along two standard parallels. Widely used for maps of the contiguous United States.

**Use cases:** East-west oriented mid-latitude regional maps (e.g., contiguous US, full map of China).

---

### 9. LambertConformal — Lambert Conformal Conic Projection

```python
ccrs.LambertConformal(
    central_longitude=-96.0,
    central_latitude=39.0,
    false_easting=0.0,
    false_northing=0.0,
    standard_parallels=(33, 45),
    globe=None,
    cutoff=-30
)
```

A conformal conic projection with no distortion on the standard parallels. The `cutoff` parameter controls the latitude at which the map is truncated, preventing the projection from extending to infinity.

**Use cases:** Aviation charts, weather maps, mid-latitude regional maps.

---

### 10. EquidistantConic — Equidistant Conic Projection

```python
ccrs.EquidistantConic(
    central_longitude=0.0,
    central_latitude=0.0,
    false_easting=0.0,
    false_northing=0.0,
    standard_parallels=(20.0, 50.0),
    globe=None
)
```

A conic projection that preserves distances along all meridians and one or two standard parallels. A compromise between equal-area and conformal.

**Use cases:** Regional maps requiring a balance of distance accuracy.

---

## III. Azimuthal Projections

### 11. AzimuthalEquidistant — Azimuthal Equidistant Projection

```python
ccrs.AzimuthalEquidistant(
    central_longitude=0.0,
    central_latitude=0.0,
    false_easting=0.0,
    false_northing=0.0,
    globe=None
)
```

Preserves correct direction and distance from the center point, but angles and areas may be distorted elsewhere.

**Use cases:** Distance analysis centered on a city, radio communication coverage maps, polar maps.

---

### 12. LambertAzimuthalEqualArea — Lambert Azimuthal Equal-Area Projection

```python
ccrs.LambertAzimuthalEqualArea(
    central_longitude=0.0,
    central_latitude=0.0,
    false_easting=0.0,
    false_northing=0.0,
    globe=None
)
```

An equal-area azimuthal projection that preserves area but distorts shape away from the center.

**Use cases:** Regional maps requiring accurate areas, continental-scale thematic maps.

---

### 13. Gnomonic — Gnomonic Projection

```python
ccrs.Gnomonic(
    central_latitude=0.0,
    central_longitude=0.0,
    globe=None
)
```

Projects from the center of the sphere onto a tangent plane. Its key feature is that all great circles (shortest paths) appear as straight lines on the map. Can only display less than a hemisphere.

**Use cases:** Route planning, seismic wave path analysis.

---

### 14. Stereographic — Stereographic Projection

```python
ccrs.Stereographic(
    central_latitude=0.0,
    central_longitude=0.0,
    false_easting=0.0,
    false_northing=0.0,
    true_scale_latitude=None,
    scale_factor=None,
    globe=None
)
```

A conformal azimuthal projection that projects from a point on the sphere onto the opposite tangent plane. Preserves local shapes; circles on the sphere remain circles on the map.

**Use cases:** Polar maps, geological structural projections (stereonet).

---

### 15. Orthographic — Orthographic Projection

```python
ccrs.Orthographic(
    central_longitude=0.0,
    central_latitude=0.0,
    globe=None
)
```

A parallel projection from infinity onto a tangent plane, creating a "view from space" visual effect. Can only display one hemisphere.

**Use cases:** Globe-view presentation maps, educational Earth diagrams.

---

## IV. Pseudocylindrical Projections

### 16. Mollweide — Mollweide Projection

```python
ccrs.Mollweide(central_longitude=0, globe=None)
```

An equal-area pseudocylindrical projection; the entire globe appears as an ellipse. Meridians are elliptical arcs; parallels are unevenly spaced horizontal lines.

**Use cases:** Global equal-area distribution maps, climate data visualization.

---

### 17. Robinson — Robinson Projection

```python
ccrs.Robinson(central_longitude=0, globe=None)
```

A compromise pseudocylindrical projection that is neither conformal nor equal-area, but produces a visually balanced and attractive result. Formerly used by the National Geographic Society for many years.

**Use cases:** Aesthetically pleasing world maps.

---

### 18. Sinusoidal — Sinusoidal Projection

```python
ccrs.Sinusoidal(
    central_longitude=0.0,
    false_easting=0.0,
    false_northing=0.0,
    globe=None
)
```

An equal-area projection where meridians are sinusoidal curves and parallels are equally spaced horizontal lines. Distortion is small near the central meridian but increases toward the edges.

**Use cases:** Equal-area maps of equatorial regions, maps of Africa and South America.

---

### 19. InterruptedGoodeHomolosine — Interrupted Goode Homolosine Projection

```python
ccrs.InterruptedGoodeHomolosine(
    central_longitude=0,
    globe=None,
    emphasis='land'
)
```

A composite equal-area projection (sinusoidal at high latitudes, Mollweide at low latitudes) that uses "interruptions" to reduce shape distortion of continents or oceans. `emphasis` can be `'land'` or `'ocean'`.

**Use cases:** Global equal-area thematic maps emphasizing land or ocean.

---

### 20. EqualEarth — Equal Earth Projection

```python
ccrs.EqualEarth(
    central_longitude=0,
    false_easting=None,
    false_northing=None,
    globe=None
)
```

A new pseudocylindrical equal-area projection published in 2018. Parallels are unevenly spaced straight lines; meridians are equally spaced arcs. Visually similar to Robinson but guarantees area accuracy.

**Requires:** Proj 5.2.0 or newer.

**Use cases:** Modern world maps that balance aesthetics and equal-area properties.

---

### 21–26. Eckert I – VI — Eckert Projection Series

The Eckert series comprises 6 projections, paired by number (non-equal-area + equal-area):

#### EckertI (non-equal-area) & EckertII (equal-area)

```python
ccrs.EckertI(central_longitude=0, false_easting=None, false_northing=None, globe=None)
ccrs.EckertII(central_longitude=0, false_easting=None, false_northing=None, globe=None)
```

Both meridians and parallels are straight lines. EckertI is equidistant; EckertII is equal-area.

#### EckertIII (non-equal-area) & EckertIV (equal-area)

```python
ccrs.EckertIII(central_longitude=0, false_easting=None, false_northing=None, globe=None)
ccrs.EckertIV(central_longitude=0, false_easting=None, false_northing=None, globe=None)
```

Parallels are straight lines; meridians are elliptical arcs (semicircles at the edges). EckertIV is commonly used for world maps.

#### EckertV (non-equal-area) & EckertVI (equal-area)

```python
ccrs.EckertV(central_longitude=0, false_easting=None, false_northing=None, globe=None)
ccrs.EckertVI(central_longitude=0, false_easting=None, false_northing=None, globe=None)
```

Parallels are straight lines; meridians are sinusoidal curves. EckertVI is commonly used for world maps.

> **Note:** All Eckert projections do not support ellipsoids (sphere model only).

**Use cases:** World thematic maps, especially when equal-area properties are needed (choose II, IV, or VI).

---

### 27. Aitoff — Aitoff Projection

```python
ccrs.Aitoff(
    central_longitude=0,
    false_easting=None,
    false_northing=None,
    globe=None
)
```

A modified azimuthal equidistant projection that balances shape and scale distortion. The entire globe appears as an ellipse; only the center point is distortion-free.

> **Note:** Does not support ellipsoids.

**Use cases:** Global overview maps.

---

### 28. Hammer — Hammer Projection

```python
ccrs.Hammer(
    central_longitude=0,
    false_easting=None,
    false_northing=None,
    globe=None
)
```

An equal-area projection similar to Aitoff but based on a modification of the Lambert Azimuthal Equal-Area projection. Compared to Mollweide, it has less distortion at the outer meridians.

> **Note:** Does not support ellipsoids.

**Use cases:** Global equal-area display maps.

---

## V. Perspective Projections

### 29. Geostationary — Geostationary Orbit Projection

```python
ccrs.Geostationary(
    central_longitude=0.0,
    satellite_height=35785831,
    false_easting=0,
    false_northing=0,
    globe=None,
    sweep_axis='y'
)
```

Simulates the perspective of a geostationary satellite looking down from 35,786 km directly above the equator. Projection coordinates are the satellite scan angle multiplied by satellite height (units: meters).

**Use cases:** Meteorological satellite data visualization (e.g., Himawari-8, GOES-16 data).

---

### 30. NearsidePerspective — Nearside Perspective Projection

```python
ccrs.NearsidePerspective(
    central_longitude=0.0,
    central_latitude=0.0,
    satellite_height=35785831,
    false_easting=0,
    false_northing=0,
    globe=None
)
```

A perspective view looking down from above any point on Earth. Unlike Geostationary, it can look down from any latitude/longitude (not limited to the equator), and the satellite height is adjustable.

**Use cases:** Custom-angle "space view" effect maps.

---

## VI. Special-purpose Projections

### 31. RotatedPole — Rotated Pole Projection

```python
ccrs.RotatedPole(
    pole_longitude=0.0,
    pole_latitude=90.0,
    central_rotated_longitude=0.0,
    globe=None
)
```

Applies a cylindrical projection after rotating Earth's "pole" to a new position. Commonly used in regional climate models to place the simulation region near the equator to reduce grid distortion.

**Use cases:** Regional climate model data (e.g., CORDEX), meteorological model output.

---

### 32. Spilhaus — Spilhaus World Ocean Projection

```python
ccrs.Spilhaus(
    rotation=45,
    false_easting=0.0,
    false_northing=0.0
)
```

An ocean-centered map based on the Adams Square projection that presents the world's oceans as a single continuous body. The two main antipodal points are located on land (southern China and Argentina), ensuring that the ocean is not split on the map.

> **Note:** This projection was introduced in Cartopy v0.23+.

**Use cases:** Oceanographic research, global ocean current visualization.

---

## VII. Polar Projections

### 33. NorthPolarStereo — North Polar Stereographic Projection

```python
ccrs.NorthPolarStereo(
    central_longitude=0.0,
    true_scale_latitude=None,
    globe=None
)
```

A stereographic projection centered on the North Pole. A convenience wrapper for `Stereographic(central_latitude=90)`.

**Use cases:** Arctic regional maps, Arctic sea ice distribution maps.

---

### 34. SouthPolarStereo — South Polar Stereographic Projection

```python
ccrs.SouthPolarStereo(
    central_longitude=0.0,
    true_scale_latitude=None,
    globe=None
)
```

A stereographic projection centered on the South Pole. A convenience wrapper for `Stereographic(central_latitude=-90)`.

**Use cases:** Antarctic maps, Antarctic research data display.

---

## VIII. Regional Projections

### 35. OSGB — British National Grid

```python
ccrs.OSGB(approx=False)
```

The British Ordnance Survey National Grid coordinate system, based on the Transverse Mercator projection using the Airy 1830 ellipsoid.

**Use cases:** Large-scale maps of the UK, British official surveying data.

---

### 36. OSNI — Northern Ireland Grid

```python
ccrs.OSNI(approx=False)
```

The coordinate system used by the Ordnance Survey of Northern Ireland, also based on the Transverse Mercator projection.

**Use cases:** Northern Ireland regional mapping.

---

### 37. EuroPP — European Regional Projection

```python
ccrs.EuroPP()
```

Uses UTM Zone 32 projection with the International 1924 ellipsoid and ED50 datum. Designed specifically for the European continent.

**Use cases:** European regional maps.

---

## IX. Common Projections for China Maps

Cartopy does not provide dedicated projection classes for China, but by properly configuring existing projection parameters, you can achieve results that meet Chinese cartographic standards and practical needs. The following are the three most commonly used projection configurations for China maps.

### Landscape China Map — AlbersEqualArea (Recommended)

```python
import cartopy.crs as ccrs

proj = ccrs.AlbersEqualArea(
    central_longitude=110,
    standard_parallels=(25, 47)
)
```

This is the preferred projection for creating **standard landscape China maps**. The central meridian at 110°E is approximately at the east-west center of China's territory, and the two standard parallels at 25°N and 47°N pass through southern and northern China respectively, minimizing area distortion across most of the country.

**Characteristics:**

- Equal-area, suitable for thematic maps (population density, GDP distribution, land use, etc.)
- China's overall shape appears natural and attractive, consistent with standard atlas presentations
- Minimum distortion between the two standard parallels

**Use cases:** China full-territory thematic maps, statistical data visualization, academic paper figures.

---

### Meteorology / Atmospheric Science — LambertConformal

```python
proj = ccrs.LambertConformal(
    central_longitude=110,
    standard_parallels=(25, 47)
)
```

The most commonly used projection in Chinese meteorology. Many numerical weather prediction models (e.g., WRF) use Lambert Conformal Conic projection by default. The parameter configuration matches the landscape equal-area setup, but the projection properties differ.

**Characteristics:**

- Conformal, preserves local shapes; directions of vector fields (wind, pressure gradients) remain correct
- Native projection of WRF, GRAPES, and other meteorological models — data aligns directly with the plot
- Not equal-area, but area distortion in mid-latitudes is very small

**Use cases:** Weather forecast maps, meteorological data visualization, WRF model output plotting, wind/pressure field analysis.

---

### Portrait China Map — AzimuthalEquidistant

```python
proj = ccrs.AzimuthalEquidistant(
    central_longitude=105,
    central_latitude=35
)
```

Used for creating **portrait-format China maps**. The center point (105°E, 35°N) is approximately at the geometric center of China's territory (near Lanzhou, Gansu). Portrait maps integrate the South China Sea islands into the main map, eliminating the need for a separate South China Sea inset and presenting all of China's territory in one view.

**Characteristics:**

- Accurate distance and direction from the center point
- Portrait layout, integrating the South China Sea islands with the mainland
- China officially began promoting portrait China maps in 2014

**Use cases:** Portrait China full maps, defense and territorial displays, scenarios requiring complete South China Sea representation.

---

### Comparison of Three Configurations

| Configuration      | Projection Class         | Key Property | Best Use                        |
| ------------------ | ------------------------ | ------------ | ------------------------------- |
| Landscape China    | `AlbersEqualArea`        | Equal-area   | Thematic maps, statistical data |
| Meteorology        | `LambertConformal`       | Conformal    | Weather maps, met models        |
| Portrait China     | `AzimuthalEquidistant`   | Equidistant  | Portrait full maps, complete South China Sea display |

---

## Projection Classification Quick Reference

| Category           | Projection Name              | Equal-area | Conformal | Recommended Use                |
| ------------------ | ---------------------------- | :--------: | :-------: | ------------------------------ |
| **Cylindrical**    | PlateCarree                  |     No     |    No     | Data coordinate transform base |
|                    | Mercator                     |     No     |   Yes     | Navigation, web maps           |
|                    | Miller                       |     No     |    No     | Global overview                |
|                    | LambertCylindrical           |    Yes     |    No     | Equal-area global map          |
|                    | TransverseMercator           |     No     |   Yes     | N-S oriented narrow strips     |
|                    | UTM                          |     No     |   Yes     | Large-scale surveying          |
|                    | ObliqueMercator              |     No     |   Yes     | Oblique strip regions          |
| **Conic**          | AlbersEqualArea              |    Yes     |    No     | Mid-latitude equal-area maps   |
|                    | LambertConformal             |     No     |   Yes     | Aviation, weather maps         |
|                    | EquidistantConic             |     No     |    No     | Distance accuracy balance      |
| **Azimuthal**      | AzimuthalEquidistant         |     No     |    No     | Distance analysis              |
|                    | LambertAzimuthalEqualArea    |    Yes     |    No     | Regional equal-area maps       |
|                    | Gnomonic                     |     No     |    No     | Great circle routes            |
|                    | Stereographic                |     No     |   Yes     | Polar, geological              |
|                    | Orthographic                 |     No     |    No     | Globe view                     |
| **Pseudocylindrical** | Mollweide                 |    Yes     |    No     | Global distribution maps       |
|                    | Robinson                     |     No     |    No     | Aesthetic world maps           |
|                    | Sinusoidal                   |    Yes     |    No     | Equatorial equal-area          |
|                    | InterruptedGoodeHomolosine   |    Yes     |    No     | Global thematic maps           |
|                    | EqualEarth                   |    Yes     |    No     | Modern equal-area world maps   |
|                    | EckertI                      |     No     |    No     | World maps                     |
|                    | EckertII                     |    Yes     |    No     | Equal-area world maps          |
|                    | EckertIII                    |     No     |    No     | World maps                     |
|                    | EckertIV                     |    Yes     |    No     | Equal-area world maps          |
|                    | EckertV                      |     No     |    No     | World maps                     |
|                    | EckertVI                     |    Yes     |    No     | Equal-area world maps          |
|                    | Aitoff                       |     No     |    No     | Global overview                |
|                    | Hammer                       |    Yes     |    No     | Global equal-area              |
| **Perspective**    | Geostationary                |     No     |    No     | Met satellite data             |
|                    | NearsidePerspective          |     No     |    No     | Space view effect              |
| **Special**        | RotatedPole                  |     No     |    No     | Climate model data             |
|                    | Spilhaus                     |     No     |    No     | Oceanography                   |
| **Polar**          | NorthPolarStereo             |     No     |   Yes     | Arctic maps                    |
|                    | SouthPolarStereo             |     No     |   Yes     | Antarctic maps                 |
| **Regional**       | OSGB                         |     No     |   Yes     | British surveying              |
|                    | OSNI                         |     No     |   Yes     | Northern Ireland               |
|                    | EuroPP                       |     No     |   Yes     | European region                |

---

## References

- Cartopy official documentation (v0.25): https://scitools.org.uk/cartopy/docs/latest/reference/projections.html
- Cartopy GitHub repository: https://github.com/SciTools/cartopy
- Proj projection library: https://proj.org/
