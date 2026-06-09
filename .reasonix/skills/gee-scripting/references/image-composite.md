# Image Filtering, Cloud Masking, and Compositing

## 1. Image Filtering

The core workflow for acquiring imagery on GEE is: load an `ImageCollection`, then progressively filter by time, space, and attributes.

```python
collection = (ee.ImageCollection("COPERNICUS/S2_SR_HARMONIZED")
    .filterDate("2024-01-01", "2025-01-01")
    .filterBounds(roi)
    .filter(ee.Filter.lt("CLOUDY_PIXEL_PERCENTAGE", 20)))
```

### Temporal Filtering: filterDate()

`filterDate(start, end)` filters by date range based on the image's `system:time_start` property. **The start date is inclusive, the end date is exclusive** — so filtering for the full year 2024 should be `filterDate("2024-01-01", "2025-01-01")`, not `"2024-12-31"`.

Both start and end can be strings, `ee.Date`, or timestamps. Beyond continuous date ranges, you can also filter by calendar fields:

| Method | Use Case | Example |
|--------|----------|---------|
| `.filterDate(start, end)` | Continuous date range | `filterDate("2024-01-01", "2025-01-01")` |
| `.filter(ee.Filter.calendarRange(start, end, field))` | Specific months/years/DOY | `calendarRange(6, 8, "month")` keeps June–August |
| `.filter(ee.Filter.dayOfYear(start, end))` | Specific DOY range | `dayOfYear(121, 273)` |

### Spatial Filtering: filterBounds()

`filterBounds(geometry)` retains images whose footprint intersects the given geometry, equivalent to `filter(ee.Filter.bounds(...))`.

**Performance note:** Very large or complex geometries (e.g., administrative boundaries with tens of thousands of vertices) can slow down filtering. If a large vector file loaded via `load_region()` has too many vertices, consider simplifying it before using it with `filterBounds`.

### Attribute Filtering: filter() + ee.Filter

For filtering on metadata attributes like cloud cover, sensor, or orbit, use `filter()` with `ee.Filter`:

| Method | Use Case | Example |
|--------|----------|---------|
| `ee.Filter.lt(name, value)` | Less than | `ee.Filter.lt("CLOUDY_PIXEL_PERCENTAGE", 20)` |
| `ee.Filter.lte(name, value)` | Less than or equal | `ee.Filter.lte("CLOUD_COVER", 30)` |
| `ee.Filter.gt(name, value)` | Greater than | `ee.Filter.gt("SUN_ELEVATION", 20)` |
| `ee.Filter.eq(name, value)` | Equal to | `ee.Filter.eq("SPACECRAFT_NAME", "Sentinel-2A")` |
| `ee.Filter.inList(name, list)` | In list | `ee.Filter.inList("MGRS_TILE", ["50TMK"])` |

**Note: Different datasets use different attribute names.** The Sentinel-2 cloud cover field is `CLOUDY_PIXEL_PERCENTAGE`, while Landsat uses `CLOUD_COVER`. Always verify the metadata field definitions for your target dataset in the GEE Data Catalog before filtering.

### Cloud Cover Pre-filtering Recommendations

Scene-level cloud cover is a statistic for the entire image — the study area may happen to be in a cloud-free zone. Setting the threshold too low will discard many usable images.

| Dataset | Cloud Cover Attribute | Suggested Starting Point |
|---------|----------------------|--------------------------|
| Sentinel-2 | `CLOUDY_PIXEL_PERCENTAGE` | 20% (10–30%) |
| Landsat 8/9 | `CLOUD_COVER` | 30% (20–40%) |

### Common Post-filtering Operations

```python
count = collection.size().getInfo()
print(f"Filtered images: {count}")
```

| Operation | Code | Use Case |
|-----------|------|----------|
| Get single scene | `collection.first()` | Only need one image |
| Sort and get best | `collection.sort("CLOUDY_PIXEL_PERCENTAGE").first()` | Get the least cloudy image |
| Composite | `collection.median()` | Composite into a representative image (see Compositing section) |

---

## 2. Cloud Masking

The core idea of cloud masking is not to delete entire images, but rather: **generate a mask from QA bands or cloud probability → use `updateMask()` to mask out cloud pixels → composite multiple images to get a cloud-free result.**

### Sentinel-2 Cloud Masking

Sentinel-2 has three cloud masking approaches:

| Approach | Principle | Pros | Cons |
|----------|-----------|------|------|
| **QA60** | Bitmask (bit 10 cloud, bit 11 cirrus) | Simple and fast, official standard example | **Unavailable from 2022-01-25 to 2024-02-28** |
| **SCL** | Scene Classification Layer (included in L2A, 20m) | Rich categories, fine-grained control, no temporal gap | Only available in L2A (SR) products, 20m resolution |
| **s2cloudless** | Cloud probability dataset + cloud shadow projection detection | Most robust, strongest cloud shadow detection | More complex to implement |

Approach selection recommendations:
- **Quick tasks, time period within QA60 availability** → QA60
- **Most cases (recommended default)** → SCL
- **Need high-quality cloud and shadow removal, zero tolerance for cloud residuals** → s2cloudless

#### Approach 1: QA60 (Simple and Fast)

Bit 10 = opaque cloud, bit 11 = cirrus. Both bits being 0 means clear sky.

```python
def mask_s2_clouds_qa60(image):
    qa = image.select("QA60")
    cloud_mask = qa.bitwiseAnd(1 << 10).eq(0)
    cirrus_mask = qa.bitwiseAnd(1 << 11).eq(0)
    return image.updateMask(cloud_mask.And(cirrus_mask))
```

**Important: QA60 has a temporal gap.** QA60 was masked out from 2022-01-25 to 2024-02-28 and cannot be used. For data within this period, use SCL or s2cloudless instead.

#### Approach 2: SCL (Recommended Default)

SCL (Scene Classification Layer) is a pixel-level classification result included in L2A products at 20m resolution, with no temporal gap.

```python
def mask_s2_clouds_scl(image):
    scl = image.select("SCL")
    mask = scl.neq(3).And(scl.neq(8)).And(scl.neq(9)).And(scl.neq(10))
    return image.updateMask(mask)
```

Full SCL class values:

| Value | Class | Commonly Removed? |
|-------|-------|-------------------|
| 0 | No data | Yes |
| 1 | Saturated/Defective | Case-by-case |
| 2 | Dark area | Case-by-case (sometimes misclassified as cloud shadow) |
| 3 | Cloud shadow | **Yes** |
| 4 | Vegetation | No |
| 5 | Bare soil | No |
| 6 | Water | No |
| 7 | Low probability cloud | Case-by-case |
| 8 | Medium probability cloud | **Yes** |
| 9 | High probability cloud | **Yes** |
| 10 | Thin cirrus | **Yes** |
| 11 | Snow/Ice | Case-by-case |

Basic cloud masking removes classes 3, 8, 9, 10. Progressively add 1, 2, 7 if cloud residuals persist. In mountainous scenes, class 11 (snow/ice) may need to be preserved to avoid false masking.

#### Approach 3: s2cloudless (Most Robust)

Uses the independent Sentinel-2 Cloud Probability dataset (`COPERNICUS/S2_CLOUD_PROBABILITY`), determines clouds via probability thresholds, and detects cloud shadows using NIR dark pixels and cloud projection direction.

```python
def get_s2_sr_cld_col(roi, start_date, end_date, cloud_filter=60):
    """Join S2 SR with cloud probability collections by system:index."""
    s2_sr = (ee.ImageCollection("COPERNICUS/S2_SR_HARMONIZED")
        .filterBounds(roi)
        .filterDate(start_date, end_date)
        .filter(ee.Filter.lte("CLOUDY_PIXEL_PERCENTAGE", cloud_filter)))

    s2_cloudless = (ee.ImageCollection("COPERNICUS/S2_CLOUD_PROBABILITY")
        .filterBounds(roi)
        .filterDate(start_date, end_date))

    return ee.ImageCollection(
        ee.Join.saveFirst("s2cloudless").apply(
            primary=s2_sr,
            secondary=s2_cloudless,
            condition=ee.Filter.equals(
                leftField="system:index", rightField="system:index")))

def add_cloud_shadow_mask(image, cloud_prob_thresh=50, nir_dark_thresh=0.15, cloud_proj_distance=1):
    """Generate cloud + cloud shadow mask based on cloud probability and shadow projection."""
    cld_prb = ee.Image(image.get("s2cloudless")).select("probability")
    is_cloud = cld_prb.gt(cloud_prob_thresh).rename("clouds")

    dark_pixels = image.select("B8").lt(nir_dark_thresh * 1e4).rename("dark_pixels")

    shadow_azimuth = ee.Number(90).subtract(
        ee.Number(image.get("MEAN_SOLAR_AZIMUTH_ANGLE")))
    cld_proj = (is_cloud
        .directionalDistanceTransform(shadow_azimuth, cloud_proj_distance * 10)
        .reproject(crs=image.select(0).projection(), scale=100)
        .select("distance").mask().rename("cloud_transform"))

    shadows = cld_proj.multiply(dark_pixels).rename("shadows")

    is_cld_shdw = is_cloud.add(shadows).gt(0)
    is_cld_shdw = (is_cld_shdw.focalMin(2).focalMax(5)
        .reproject(crs=image.select(0).projection(), scale=20)
        .rename("cloudmask"))

    return image.addBands(is_cld_shdw)

def apply_cloud_shadow_mask(image):
    return image.select("B.*").updateMask(image.select("cloudmask").Not())
```

Usage:

```python
s2_sr_cld_col = get_s2_sr_cld_col(roi, "2024-01-01", "2025-01-01")
clean_col = (s2_sr_cld_col
    .map(lambda img: add_cloud_shadow_mask(img))
    .map(apply_cloud_shadow_mask))
```

Key parameters:
- `cloud_prob_thresh` (default 50): Lower = stricter, more thorough cloud removal but may cause false masking
- `nir_dark_thresh` (default 0.15): NIR dark pixel threshold for cloud shadow detection
- `cloud_proj_distance` (default 1 km): Cloud shadow search range

### Landsat Cloud Masking

The official recommendation is that **most users should use `QA_PIXEL` for cloud identification**. Complete Landsat SR preprocessing should also include `QA_RADSAT` (radiometric saturation masking) and scaling factor correction.

```python
def mask_landsat_sr(image):
    # QA_PIXEL: remove fill, dilated cloud, cirrus, cloud, cloud shadow (bits 0-4)
    qa_mask = image.select("QA_PIXEL").bitwiseAnd(int("11111", 2)).eq(0)
    # QA_RADSAT: remove radiometrically saturated pixels
    saturation_mask = image.select("QA_RADSAT").eq(0)
    # Scaling factor correction: DN → reflectance
    optical = image.select("SR_B.").multiply(0.0000275).add(-0.2)

    return (image
        .addBands(optical, None, True)
        .updateMask(qa_mask)
        .updateMask(saturation_mask))
```

QA_PIXEL bit encoding:

| Bit | Meaning | Description |
|-----|---------|-------------|
| bit 0 | Fill | No-data areas |
| bit 1 | Dilated cloud | Cloud edge buffer zone |
| bit 2 | Cirrus | High-altitude thin clouds |
| bit 3 | Cloud | **Core cloud detection** |
| bit 4 | Cloud shadow | **Core cloud shadow detection** |
| bit 5 | Snow | May preserve for snow analysis |
| bit 7 | Water | Reference for water analysis |

Basic cloud masking uses `int("11111", 2)` (bits 0–4) covering fill, dilated cloud, cirrus, cloud, and cloud shadow. For cloud and shadow only, use bits 3 and 4.

**Landsat scaling factors:** Collection 2 SR optical band DN values require `× 0.0000275 + (−0.2)` to convert to reflectance (0–1). Thermal infrared bands (ST_B10) use `× 0.00341802 + 149.0` to convert to Kelvin temperature.

### Cloud Masking Troubleshooting

| Issue | Cause | Solution |
|-------|-------|----------|
| Visible cloud pixels remain after masking | Mask not strict enough | S2: add SCL classes or switch to s2cloudless; Landsat: ensure bit 1 is included |
| Cloud shadows not fully removed | Imperfect shadow detection | S2: use s2cloudless; Landsat: ensure bit 4 is included |
| Many pixels falsely masked in mountainous areas | Snow/ice misclassified as cloud | Use snow-free season data, or preserve snow class |
| Large gaps after cloud masking | Insufficient valid observations | See "Quality Check and Gap Filling" section below |

---

## 3. Compositing

Compositing is the process of combining multiple cloud-masked images into a single complete image. For each pixel in the study area, GEE selects or computes a value from the image stack according to a specified rule, producing a spatially continuous result.

### Method Selection

| Goal | Recommended Method |
|------|-------------------|
| **Complete coverage, preserving real observation texture** | `mosaic()` or `qualityMosaic()` |
| **Robust representative cloud-free image** | `median()` (preferred) or `mean()` |
| **Discrete classification data compositing** | `mode()` |

### mosaic(): Sequential Mosaicking

Composites in collection order — **later images have higher priority**. Where a later image is masked at a location, the valid pixel from an earlier image fills in.

```python
composite = clean_col.mosaic()
```

- **Pros:** Fast mosaicking of images from different spatial locations, automatically fills gaps using masks
- **Cons:** Order-dependent, seams and brightness inconsistencies may appear in overlap areas
- **Use for:** Spatial mosaicking, scenes with known priority ordering

Sort before mosaicking to control priority:

```python
composite = clean_col.sort("CLOUDY_PIXEL_PERCENTAGE").mosaic()
```

### median(): Per-pixel Median (Recommended Default)

Computes the median of all valid observations for each pixel. Naturally resistant to outliers — clouds (abnormally high values) and shadows (abnormally low values) are excluded by the median.

```python
composite = clean_col.median()
```

- **Pros:** Robust, resistant to residual clouds and outliers; first choice for annual/seasonal representative images
- **Cons:** Statistical value rather than an actual single-day observation; median of different bands may come from different dates, potentially producing spectral combinations that don't exist in reality
- **Use for:** Annual/seasonal cloud-free representative images, pre-classification base maps, tasks requiring high consistency

### mean(): Per-pixel Mean

```python
composite = clean_col.mean()
```

- **Pros:** Reflects average conditions; stable results when observations are numerous and high quality
- **Cons:** More sensitive to outliers than `median()`; details become blurred in high-contrast areas
- **Use for:** Quality-controlled data, analyses requiring "average state", long-term average background maps

### qualityMosaic(): Best-pixel Selection by Quality Band

For each pixel, selects the scene with the **highest quality band value** and takes all bands from that scene. Each pixel comes from a real observation, so edges and textures look more natural than statistical composites.

- **Pros:** Preserves real observation texture; flexible definition of "best"
- **Cons:** Depends on quality band definition being reasonable
- **Use for:** Best-pixel compositing, most recent cloud-free image, greenest vegetation

**Lowest cloud probability pixel** (low cloud probability = high quality, needs inversion):

```python
def add_cloud_quality(image):
    quality = image.select("MSK_CLDPRB").multiply(-1).rename("quality")
    return image.addBands(quality)

composite = clean_col.map(add_cloud_quality).qualityMosaic("quality").clip(roi)
```

**Greenest pixel** (highest NDVI):

```python
def add_ndvi(image):
    return image.addBands(image.normalizedDifference(["B8", "B4"]).rename("NDVI"))

composite = clean_col.map(add_ndvi).qualityMosaic("NDVI").clip(roi)
```

**Most recent observation** (highest timestamp):

```python
def add_time_band(image):
    return image.addBands(image.metadata("system:time_start"))

composite = clean_col.map(add_time_band).qualityMosaic("system:time_start").clip(roi)
```

### mode(): Per-pixel Mode

Returns the most frequently occurring value for each pixel. Not suitable for continuous reflectance bands; suitable for discrete classification data.

```python
mode_img = clean_col.select("SCL").mode()
```

- **Use for:** SCL classification, land cover classes, multi-temporal classification result integration

### Other Reduction Methods

| Method | Use Case |
|--------|----------|
| `count()` | Number of valid observations per pixel (see Quality Check section) |
| `min()` / `max()` | Extremes extraction (minimum temperature, maximum NDVI) |
| `sum()` | Cumulative values (accumulated precipitation, growing degree days) |

### Seasonal Compositing

Group annual data by season and composite:

```python
seasons = {
    "spring": ("2024-03-01", "2024-06-01"),
    "summer": ("2024-06-01", "2024-09-01"),
    "autumn": ("2024-09-01", "2024-12-01"),
    "winter_1": ("2024-01-01", "2024-03-01"),
    "winter_2": ("2024-12-01", "2025-01-01"),
}

for name, (start, end) in seasons.items():
    with heartbeat(f"Compositing {name}"):
        seasonal = clean_col.filterDate(start, end).median().clip(roi)
    download_image(seasonal, f"/path/to/{name}_composite.tif", roi, scale=10)
    print(f"{name} composite saved")
```

---

## 4. Quality Check and Gap Filling

After compositing, check whether the study area has gaps (masked pixels). Gaps are usually not bugs — they mean that pixel had no valid observations after cloud masking within the selected time period. `clip(roi)` only masks data outside the geometry; if a location within the geometry originally had no value, it remains masked.

### Causes of Gaps

| Cause | Description |
|-------|-------------|
| **Time window too short** | After cloud masking, many locations have zero valid images remaining |
| **Mask conditions too strict** | Too many classes removed simultaneously, too few valid pixels |
| **Incomplete coverage** | Images from different orbits/paths don't fully cover the study area |

### Checking for Gaps

**Use `check_coverage()` to quickly assess coverage (recommended):**

```python
report = check_coverage(composite, roi)
# → Coverage: 99.5% (good) | valid: 12438/12500
```

If the report says "fair" or "poor", adjust parameters before downloading.

### Fixing Gaps

**1. Expand the time range** — the most fundamental approach.

**2. Relax mask conditions** — only remove obvious clouds and shadows; don't delete all suspect classes.

**3. Image-level filtering first, then pixel-level cloud masking** — use `CLOUDY_PIXEL_PERCENTAGE` to first discard globally poor images, keeping locally usable ones for pixel-level masking.

**4. Run `check_coverage()` before exporting** — decide whether parameters need adjustment based on coverage.

**5. Fill gaps with backup imagery (unmask):**

```python
with heartbeat("Filling holes"):
    backup = (ee.ImageCollection("COPERNICUS/S2_SR_HARMONIZED")
        .filterDate("2023-01-01", "2025-01-01")
        .filterBounds(roi)
        .map(mask_s2_clouds_scl)
        .median().clip(roi))
    filled = composite.unmask(backup, sameFootprint=False)
```

`unmask(backup)` fills masked locations in the primary image with the backup image. `sameFootprint=False` allows filling to extend beyond the primary image's footprint.

---

## Complete Workflow Example

```python
from geocode import init_gee, load_region, download_image, check_coverage, heartbeat
import ee

init_gee("project-id")
roi = load_region("/path/to/boundary.shp").geometry()

# Cloud masking function
def mask_s2_clouds_scl(image):
    scl = image.select("SCL")
    mask = scl.neq(3).And(scl.neq(8)).And(scl.neq(9)).And(scl.neq(10))
    return image.updateMask(mask)

# 1. Filter and mask
with heartbeat("Filtering and masking"):
    clean_col = (ee.ImageCollection("COPERNICUS/S2_SR_HARMONIZED")
        .filterDate("2024-01-01", "2025-01-01")
        .filterBounds(roi)
        .filter(ee.Filter.lt("CLOUDY_PIXEL_PERCENTAGE", 20))
        .map(mask_s2_clouds_scl))
    img_count = clean_col.size().getInfo()
print(f"Images after filtering: {img_count}")

if img_count == 0:
    print("WARNING: No images found. Try expanding the date range or raising the cloud threshold.")
else:
    # 2. Composite
    with heartbeat("Computing composite"):
        composite = clean_col.median().clip(roi)

    # 3. Quality check
    report = check_coverage(composite, roi)

    # 4. Fill gaps (if needed)
    if report["coverage"] < 0.99:
        print("Filling gaps with wider time range...")
        with heartbeat("Computing backup and filling"):
            backup = (ee.ImageCollection("COPERNICUS/S2_SR_HARMONIZED")
                .filterDate("2023-01-01", "2025-01-01")
                .filterBounds(roi)
                .map(mask_s2_clouds_scl)
                .median().clip(roi))
            composite = composite.unmask(backup, sameFootprint=False)

    # 5. Export
    download_image(composite, "/path/to/composite.tif", roi, scale=10)
    print("Done.")
```
