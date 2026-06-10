from __future__ import annotations

import argparse
import os
import sys
from pathlib import Path


SCRIPT_DIR = Path(__file__).resolve().parent
if sys.path and Path(sys.path[0]).resolve() == SCRIPT_DIR:
    sys.path.pop(0)

import ee
import geemap


PROJECT_ID = (
    os.environ.get("EE_PROJECT")
    or os.environ.get("GOOGLE_CLOUD_PROJECT")
    or os.environ.get("EE_PROJECT_ID")
    or "PROJECT_ID"
)
ROI_ASSET = "users/your_username/roi"
START_DATE = "2024-06-01"
END_DATE = "2024-09-30"
EXPORT_FOLDER = "GEEMu_exports"
EXPORT_DESCRIPTION = "sentinel2_ndvi_median"
SCALE = 10
CRS = "EPSG:4326"
WORKLOAD_TAG = "geemu-sentinel2-ndvi"


def resolve_project(cli_project: str | None) -> str | None:
    project = cli_project or PROJECT_ID
    return None if project == "PROJECT_ID" else project


def require_project(cli_project: str | None) -> str:
    project = resolve_project(cli_project)
    if project:
        return project
    raise RuntimeError(
        "Cloud Project ID is required before any Earth Engine request. "
        "Run with --project PROJECT_ID or set EE_PROJECT/GOOGLE_CLOUD_PROJECT/EE_PROJECT_ID."
    )


def initialize_ee(project: str) -> None:
    try:
        geemap.ee_initialize(project=project)
        ee.data.getAssetRoots()
    except Exception as exc:
        raise RuntimeError(
            "Earth Engine/geemap initialization failed. Check authentication, proxy, "
            "Cloud Project permissions, then retry."
        ) from exc
    try:
        ee.data.setDefaultWorkloadTag(WORKLOAD_TAG)
    except Exception:
        pass


def mask_s2_scl(image: ee.Image) -> ee.Image:
    scl = image.select("SCL")
    clear = (
        scl.neq(3)
        .And(scl.neq(8))
        .And(scl.neq(9))
        .And(scl.neq(10))
        .And(scl.neq(11))
    )
    return image.updateMask(clear).copyProperties(image, image.propertyNames())


def add_ndvi(image: ee.Image) -> ee.Image:
    ndvi = image.normalizedDifference(["B8", "B4"]).rename("NDVI")
    return image.addBands(ndvi).copyProperties(image, image.propertyNames())


def build_ndvi_composite(roi: ee.FeatureCollection) -> ee.Image:
    exact_region = roi.geometry()
    search_region = exact_region.bounds(maxError=100)

    collection = (
        ee.ImageCollection("COPERNICUS/S2_SR_HARMONIZED")
        .filterBounds(search_region)
        .filterDate(START_DATE, END_DATE)
        .select(["B2", "B3", "B4", "B8", "SCL"])
        .map(mask_s2_scl)
        .map(add_ndvi)
    )

    return collection.select("NDVI").median().rename("NDVI").clip(exact_region)


def harmonize_export_dtype(image: ee.Image) -> ee.Image:
    """Cast every band to one dtype (Float32) before export. A multi-band export
    fails with "Exported bands must have compatible data types; found
    inconsistent types: Float32 and Byte" when a float index band is mixed with
    a Byte mask/class band. NDVI alone is already float, but keeping this call
    makes the workflow safe if you add mask or class bands later.
    """
    return image.toFloat()


def start_export(image: ee.Image, roi: ee.FeatureCollection) -> ee.batch.Task:
    task = ee.batch.Export.image.toDrive(
        image=harmonize_export_dtype(image),
        description=EXPORT_DESCRIPTION,
        folder=EXPORT_FOLDER,
        fileNamePrefix=EXPORT_DESCRIPTION,
        region=roi.geometry(),
        scale=SCALE,
        crs=CRS,
        maxPixels=1e13,
    )
    task.start()
    return task


def print_dry_run() -> None:
    print(f"case={EXPORT_DESCRIPTION}")
    print(f"roi_asset={ROI_ASSET}")
    print("project=PROJECT_ID (required before online Earth Engine run)")
    print(f"dataset=COPERNICUS/S2_SR_HARMONIZED  time={START_DATE}/{END_DATE}")
    print(f"scale={SCALE}  crs={CRS}")
    print("cloud_mask=SCL not in {3,8,9,10,11}")
    print("NDVI = (B8 - B4) / (B8 + B4) -> median composite -> clip(roi)")
    print(f"export=Drive folder '{EXPORT_FOLDER}' (only with --export)")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Sentinel-2 median NDVI composite example.")
    parser.add_argument("--dry-run", action="store_true", help="Print parameters without Earth Engine auth.")
    parser.add_argument("--project", help="Earth Engine Cloud Project ID.")
    parser.add_argument("--export", action="store_true", help="Start the Drive export task.")
    parser.add_argument("--no-map", action="store_true", help="Skip the geemap preview.")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    if args.dry_run:
        print_dry_run()
        return 0

    try:
        project = require_project(args.project)
        initialize_ee(project)
    except RuntimeError as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 2

    roi = ee.FeatureCollection(ROI_ASSET)
    ndvi = build_ndvi_composite(roi)

    if not args.no_map:
        _map = geemap.Map()
        _map.centerObject(roi, 9)
        _map.addLayer(ndvi, {"min": 0, "max": 0.8, "palette": ["brown", "yellow", "green"]}, "NDVI")

    if args.export:
        task = start_export(ndvi, roi)
        print(f"Started export task: {task.id}")
        print("Check the Earth Engine Tasks tab or ee.data.getTaskStatus(task.id).")
    else:
        print("Composite built. Re-run with --export to start the Drive export task.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
