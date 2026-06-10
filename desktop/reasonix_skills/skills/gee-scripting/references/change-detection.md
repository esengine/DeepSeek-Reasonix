# Change Detection

## 1. Change Detection Methods

GEE does not package change detection as a single module. Two-period change is typically achieved through image arithmetic, classification workflows, or multivariate transforms; continuous change detection has dedicated `TemporalSegmentation` algorithms.

### Method Comparison

| Method | Principle | Pros | Cons | Use Case |
|--------|-----------|------|------|----------|
| **Differencing** | `after.subtract(before)` | Simple and intuitive, thresholds are easy to interpret | Can only answer "how much changed" | Vegetation degradation, water body expansion, and other well-defined changes |
| **Ratio** | `after.divide(before)` | More stable for SAR data | No clear advantage for optical data | Sentinel-1 SAR, flood monitoring |
| **Post-classification comparison** | Classify two periods separately, then compare pixel by pixel | Can answer "from what to what" | Error accumulation, depends on classification quality of both periods | Analysis requiring transition direction (forest → built-up, etc.) |
| **PCA** | Principal component transform after stacking two periods of multi-band data | Integrates multi-band information, highlights dominant changes | Physical meaning of principal components is not intuitive | Complex change components where a single index is insufficient |
| **MAD/iMAD** | Maximize correlation structure between two periods then difference | More rigorous multi-spectral change detection | Complex implementation, high understanding threshold | Unknown change types, need to fully leverage multi-band information |

### How to Choose

- **Well-defined change target** (vegetation, water, buildings) → Differencing, using the corresponding index
- **SAR data** → Ratio method
- **Need transition direction** (from class A to class B) → Post-classification comparison
- **Unknown change type, multi-band** → PCA or MAD/iMAD
- **Long-term disturbance and recovery** → LandTrendr / CCDC (see Section 6)

---

## 2. Key Requirements for Two-Period Image Comparison

### Same-Season Comparison

The two periods of imagery must come from **the same season (same DOY window)** as much as possible. Otherwise, phenological differences (crop growth stages, leaf area changes, senescence cycles) will be misidentified as land surface changes. In GEE, `calendarRange` or `dayOfYear` filters typically control the seasonal window, followed by compositing.

### Radiometric Consistency

- **Same sensor, same product**: Use surface reflectance (SR) products, preprocessed with official scaling factors and QA masks
- **Cross-sensor**: Simply "both being reflectance" may not be directly comparable; relative radiometric normalization based on invariant pixels or histogram matching may be needed

### Cloud Masking Quality

Incomplete cloud masking causes numerous false changes in the difference image (clouds/shadows mistaken for "change"). Both periods should undergo rigorous pixel-level cloud masking before compositing.

**Additional SAR data requirements:** Ensure consistent **orbit direction and relative orbit number**; otherwise, incidence angle differences will contaminate the change results.

### Two-Period Image Construction Template

```python
from geocode import init_gee, load_region, heartbeat
import ee

init_gee("project-id")
roi = load_region("/path/to/boundary.shp").geometry()

def mask_s2_clouds_scl(image):
    scl = image.select("SCL")
    mask = scl.neq(3).And(scl.neq(8)).And(scl.neq(9)).And(scl.neq(10))
    return image.updateMask(mask)

# Same season, same filtering conditions
cloud_filter = ee.Filter.lt("CLOUDY_PIXEL_PERCENTAGE", 20)

with heartbeat("Building before composite"):
    before = (ee.ImageCollection("COPERNICUS/S2_SR_HARMONIZED")
        .filterDate("2020-06-01", "2020-09-01")
        .filterBounds(roi).filter(cloud_filter)
        .map(mask_s2_clouds_scl)
        .median().clip(roi))

with heartbeat("Building after composite"):
    after = (ee.ImageCollection("COPERNICUS/S2_SR_HARMONIZED")
        .filterDate("2024-06-01", "2024-09-01")
        .filterBounds(roi).filter(cloud_filter)
        .map(mask_s2_clouds_scl)
        .median().clip(roi))
```

---

## 3. Change Index Selection

Which index to use depends on what type of change you want to detect.

### Common Indices

| Change Type | Recommended Index | Difference Calculation | Description |
|-------------|-------------------|----------------------|-------------|
| Vegetation degradation/recovery | NDVI | `after_ndvi - before_ndvi` | Most common vegetation change index |
| Fire/severe disturbance | NBR / dNBR | `before_nbr - after_nbr` | Note: burn severity conventionally uses pre-post, not post-pre |
| Urban expansion | NDBI | `after_ndbi - before_ndbi` | Can also use Dynamic World's built probability |
| Water body change | NDWI / MNDWI | `after_mndwi - before_mndwi` | MNDWI better suppresses building interference |
| Uncertain/multiple types | Multi-band difference/PCA | Full-band subtract or PCA transform | Not committing to a single index |

### Calculation Method

Use `normalizedDifference()` to construct indices within a single period, and `subtract()` for between-period differencing:

```python
# NDVI difference
before_ndvi = before.normalizedDifference(["B8", "B4"]).rename("NDVI")
after_ndvi = after.normalizedDifference(["B8", "B4"]).rename("NDVI")
dndvi = after_ndvi.subtract(before_ndvi).rename("dNDVI")

# NBR difference (burn convention: pre - post)
before_nbr = before.normalizedDifference(["B8", "B12"]).rename("NBR")
after_nbr = after.normalizedDifference(["B8", "B12"]).rename("NBR")
dnbr = before_nbr.subtract(after_nbr).rename("dNBR")
```

`normalizedDifference()` will mask the output when either input band has negative values. To avoid this, use `expression()` instead:

```python
before_mndwi = before.expression(
    "(G - SWIR) / (G + SWIR)",
    {"G": before.select("B3"), "SWIR": before.select("B11")}
).rename("MNDWI")
```

---

## 4. Threshold Determination

There is no universal standard for how large a difference constitutes "real change" — it depends on the index type, land cover type, sensor noise, and degree of seasonal control.

### Common Threshold Methods

| Method | GEE Implementation | Use Case |
|--------|-------------------|----------|
| **Mean ± n×StdDev** | `Reducer.mean()` + `Reducer.stdDev()` | Background noise is stable, change is in the distribution tails |
| **Percentiles** | `Reducer.percentile([5, 95])` | Non-normal distribution, capturing only the most extreme changes |
| **Rule-based threshold** | Set a fixed value directly | Well-defined target, prior knowledge available |
| **Otsu automatic threshold** | First `Reducer.histogram()`, then manually implement Otsu | Automatic segmentation of bimodal distributions |

GEE **has no built-in Otsu function** — it must be manually implemented based on `Reducer.histogram()`.

### Standard Deviation Threshold Example

```python
with heartbeat("Computing threshold statistics"):
    stats = dndvi.reduceRegion(
        reducer=ee.Reducer.mean().combine(
            ee.Reducer.stdDev(), sharedInputs=True),
        geometry=roi, scale=10, maxPixels=1e9
    ).getInfo()

mean_val = stats["dNDVI_mean"]
std_val = stats["dNDVI_stdDev"]
print(f"dNDVI mean: {mean_val:.4f}, std: {std_val:.4f}")

# 2σ threshold
threshold_pos = mean_val + 2 * std_val
threshold_neg = mean_val - 2 * std_val

change_increase = dndvi.gt(threshold_pos)
change_decrease = dndvi.lt(threshold_neg)
```

### Percentile Threshold Example

```python
with heartbeat("Computing percentile thresholds"):
    pct = dndvi.reduceRegion(
        reducer=ee.Reducer.percentile([5, 95]),
        geometry=roi, scale=10, maxPixels=1e9
    ).getInfo()

p5 = pct["dNDVI_p5"]
p95 = pct["dNDVI_p95"]
print(f"5th percentile: {p5:.4f}, 95th percentile: {p95:.4f}")

change_decrease = dndvi.lt(p5)
change_increase = dndvi.gt(p95)
```

---

## 5. Change Area Statistics

Convert the change result to a binary or multi-class image, then combine `pixelArea()` and `reduceRegion()` to compute areas.

### Binary Change Area

```python
with heartbeat("Calculating change area"):
    change_area = (ee.Image.pixelArea()
        .updateMask(change_decrease)  # Keep only change pixels
        .reduceRegion(
            reducer=ee.Reducer.sum(),
            geometry=roi, scale=10, maxPixels=1e9
        ).get("area").getInfo())

print(f"Decrease area: {change_area / 1e6:.2f} km²")
```

### Grouped Statistics by Change Type

Encode changes into multiple categories, then use a grouped reducer to compute areas for all types at once:

```python
# 0=decrease, 1=unchanged, 2=increase
change_type = (ee.Image(1)
    .where(dndvi.lt(threshold_neg), 0)
    .where(dndvi.gt(threshold_pos), 2)
    .rename("change_type"))

with heartbeat("Calculating area by change type"):
    area_stats = (ee.Image.pixelArea()
        .addBands(change_type)
        .reduceRegion(
            reducer=ee.Reducer.sum().group(
                groupField=1, groupName="change_type"),
            geometry=roi, scale=10, maxPixels=1e9
        ).getInfo())

for group in area_stats["groups"]:
    type_name = {0: "Decrease", 1: "Unchanged", 2: "Increase"}[group["change_type"]]
    area_km2 = group["sum"] / 1e6
    print(f"{type_name}: {area_km2:.2f} km²")
```

---

## 6. Continuous Change Detection

When you need to analyze **multi-year disturbance and recovery trajectories** rather than simple two-period comparison, use GEE's official `TemporalSegmentation` algorithms.

### Available Algorithms

| Algorithm | API | Principle | Use Case |
|-----------|-----|-----------|----------|
| **LandTrendr** | `ee.Algorithms.TemporalSegmentation.LandTrendr` | Segments time series into line segments, detecting disturbance and recovery | Forest disturbance, vegetation recovery trajectories |
| **CCDC** | `ee.Algorithms.TemporalSegmentation.Ccdc` | Iteratively fits harmonic models, detects temporal breakpoints | Continuous land cover monitoring, seasonal change |
| **EWMA-CD** | `ee.Algorithms.TemporalSegmentation.Ewmacd` | Harmonic fitting + residual control charts | Near real-time change detection |

### Differences from Two-Period Comparison

| | Two-Period Comparison | Continuous Change Detection |
|---|----------------------|----------------------------|
| Input | Two images/composites | Multi-year time series |
| Question answered | How much changed, in what direction | When it changed, gradual vs. abrupt, whether recovery occurred |
| Pros | Simple, fast, interpretable | Richer information, resistant to occasional disturbances |
| Cons | Depends on image timing, susceptible to occasional factors | Complex preprocessing, high requirements for temporal completeness |

### LandTrendr Minimal Example

```python
# annual_collection: one image per year, first band used for breakpoint detection
with heartbeat("Running LandTrendr"):
    lt = ee.Algorithms.TemporalSegmentation.LandTrendr(
        timeSeries=annual_collection,
        maxSegments=6,
        spikeThreshold=0.9,
        vertexCountOvershoot=3,
        preventOneYearRecovery=True,
        recoveryThreshold=0.25,
        pvalThreshold=0.05,
        bestModelProportion=0.75,
        minObservationsNeeded=6
    )
```

### CCDC Minimal Example

```python
with heartbeat("Running CCDC"):
    ccdc = ee.Algorithms.TemporalSegmentation.Ccdc(
        collection=ts_collection,
        breakpointBands=["NDVI"],
        tmaskBands=["green", "swir"],
        minObservations=6,
        chiSquareProbability=0.99,
        minNumOfYearsScaler=1.33,
        dateFormat=1
    )
```

Continuous change detection requires preparing a consistent annual or temporal composite image collection first, with significantly higher preprocessing requirements than two-period comparison.

---

## Complete Workflow Example (Two-Period NDVI Change Detection)

```python
from geocode import init_gee, load_region, download_image, heartbeat
import ee

init_gee("project-id")
roi = load_region("/path/to/boundary.shp").geometry()

# Cloud masking function
def mask_s2_clouds_scl(image):
    scl = image.select("SCL")
    mask = scl.neq(3).And(scl.neq(8)).And(scl.neq(9)).And(scl.neq(10))
    return image.updateMask(mask)

# 1. Build same-season two-period composites
cloud_filter = ee.Filter.lt("CLOUDY_PIXEL_PERCENTAGE", 20)

with heartbeat("Building before composite (2020 summer)"):
    before = (ee.ImageCollection("COPERNICUS/S2_SR_HARMONIZED")
        .filterDate("2020-06-01", "2020-09-01")
        .filterBounds(roi).filter(cloud_filter)
        .map(mask_s2_clouds_scl)
        .median().clip(roi))

with heartbeat("Building after composite (2024 summer)"):
    after = (ee.ImageCollection("COPERNICUS/S2_SR_HARMONIZED")
        .filterDate("2024-06-01", "2024-09-01")
        .filterBounds(roi).filter(cloud_filter)
        .map(mask_s2_clouds_scl)
        .median().clip(roi))

# 2. Compute NDVI difference
before_ndvi = before.normalizedDifference(["B8", "B4"]).rename("NDVI")
after_ndvi = after.normalizedDifference(["B8", "B4"]).rename("NDVI")
dndvi = after_ndvi.subtract(before_ndvi).rename("dNDVI")

# 3. Compute thresholds
with heartbeat("Computing change statistics"):
    stats = dndvi.reduceRegion(
        reducer=ee.Reducer.mean().combine(
            ee.Reducer.stdDev(), sharedInputs=True),
        geometry=roi, scale=10, maxPixels=1e9
    ).getInfo()

mean_val = stats["dNDVI_mean"]
std_val = stats["dNDVI_stdDev"]
threshold_pos = mean_val + 2 * std_val
threshold_neg = mean_val - 2 * std_val
print(f"dNDVI mean: {mean_val:.4f}, std: {std_val:.4f}")
print(f"Thresholds: decrease < {threshold_neg:.4f}, increase > {threshold_pos:.4f}")

# 4. Generate change classification map (0=decrease, 1=unchanged, 2=increase)
change_type = (ee.Image(1)
    .where(dndvi.lt(threshold_neg), 0)
    .where(dndvi.gt(threshold_pos), 2)
    .rename("change_type"))

# 5. Calculate change areas
with heartbeat("Calculating change areas"):
    area_stats = (ee.Image.pixelArea()
        .addBands(change_type)
        .reduceRegion(
            reducer=ee.Reducer.sum().group(
                groupField=1, groupName="change_type"),
            geometry=roi, scale=10, maxPixels=1e9
        ).getInfo())

for group in area_stats["groups"]:
    name = {0: "Decrease", 1: "Unchanged", 2: "Increase"}[group["change_type"]]
    print(f"{name}: {group['sum'] / 1e6:.2f} km²")

# 6. Export
download_image(dndvi, "/path/to/dndvi.tif", roi, scale=10)
download_image(change_type, "/path/to/change_type.tif", roi, scale=10, dtype="uint8")
print("Change detection complete.")
```
