# Single-Date Image Selection

When the user needs a real single-date observation (not a multi-date composite), use this workflow. Typical use cases: disaster monitoring, specific-event snapshots, phenological studies requiring exact acquisition dates.

**When to use single-date vs composite:**

| Scenario | Recommended |
|----------|-------------|
| Annual/seasonal basemap, classification | `median()` composite |
| Disaster before/after comparison | Single-date image |
| Specific event snapshot | Single-date image |
| Phenological study (exact date matters) | Single-date image |

## Strategy

Do NOT iterate through every candidate date — that causes hundreds of server round-trips and is extremely slow. Instead:

1. Group images by date, compute mean cloud cover per date (server-side)
2. Sort by cloud cover, take the top N candidates
3. For each candidate, mosaic + cloud mask + `check_coverage()`
4. Pick the first date that meets the coverage threshold, or the best overall

## Example: Best Single-Date Sentinel-2 Image

```python
from geocode import init_gee, load_region, download_image, check_coverage, heartbeat
import ee

init_gee("project-id")
roi = load_region("/path/to/boundary.shp").geometry()

# Cloud masking
def mask_s2_clouds_scl(image):
    scl = image.select("SCL")
    mask = scl.neq(3).And(scl.neq(8)).And(scl.neq(9)).And(scl.neq(10))
    return image.updateMask(mask)

# 1. Filter collection
with heartbeat("Filtering collection"):
    collection = (
        ee.ImageCollection("COPERNICUS/S2_SR_HARMONIZED")
        .filterDate("2024-01-01", "2025-01-01")
        .filterBounds(roi)
        .filter(ee.Filter.lt("CLOUDY_PIXEL_PERCENTAGE", 30))
    )
    count = collection.size().getInfo()
print(f"Total images: {count}")

# 2. Rank dates by mean cloud cover (single server call)
with heartbeat("Ranking candidate dates"):
    date_cloud_list = (
        collection
        .map(lambda img: ee.Feature(None, {
            "date": img.date().format("YYYY-MM-dd"),
            "cloud": img.get("CLOUDY_PIXEL_PERCENTAGE"),
        }))
        .reduceColumns(
            reducer=ee.Reducer.mean().group(groupField=0, groupName="date"),
            selectors=["date", "cloud"],
        )
        .get("groups")
        .getInfo()
    )

    candidates = [
        {"date": g["date"], "mean_cloud": g["mean"]}
        for g in date_cloud_list
    ]
    candidates.sort(key=lambda x: x["mean_cloud"])
    top_candidates = candidates[:10]

print(f"Candidate dates: {len(candidates)}, checking top {len(top_candidates)}")

# 3. Evaluate top candidates with check_coverage
best = None
for i, c in enumerate(top_candidates, 1):
    d = c["date"]
    next_day = ee.Date(d).advance(1, "day")

    with heartbeat(f"Evaluating {i}/{len(top_candidates)}: {d}"):
        mosaic = (
            collection.filterDate(d, next_day)
            .map(mask_s2_clouds_scl)
            .mosaic()
            .clip(roi)
        )
        report = check_coverage(mosaic, roi)

    c["coverage"] = report["coverage"]
    print(f"{d} | cloud={c['mean_cloud']:.1f}% | coverage={report['coverage']:.1%}", flush=True)

    if best is None or c["coverage"] > best["coverage"]:
        best = c

    # Early exit if coverage is good enough
    if report["coverage"] >= 0.99:
        break

print(f"Best date: {best['date']} (coverage={best['coverage']:.1%}, cloud={best['mean_cloud']:.1f}%)")

# 4. Download the best date
next_day = ee.Date(best["date"]).advance(1, "day")
with heartbeat("Building final image"):
    best_img = (
        collection.filterDate(best["date"], next_day)
        .map(mask_s2_clouds_scl)
        .mosaic()
        .select(["B4", "B3", "B2"])
        .clip(roi)
    )

download_image(best_img, "/path/to/output.tif", roi, scale=10)
print("Done.")
```

## Key Differences from the Composite Workflow

- Uses `mosaic()` (single-date) instead of `median()` (multi-date)
- Only checks top candidates by cloud cover, not all dates
- Early exit once a good-enough date is found
- Result is a real observation from one date, not a statistical composite
