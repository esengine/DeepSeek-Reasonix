from __future__ import annotations

import argparse
import json
import os
import sys
from dataclasses import dataclass, asdict
from pathlib import Path


SCRIPT_DIR = Path(__file__).resolve().parent
if sys.path and Path(sys.path[0]).resolve() == SCRIPT_DIR:
    sys.path.pop(0)

import ee
import geemap


CASE_NAME = "NSW_dNBR_Recovery"
PROJECT_ID = (
    os.environ.get("EE_PROJECT")
    or os.environ.get("GOOGLE_CLOUD_PROJECT")
    or os.environ.get("EE_PROJECT_ID")
    or "PROJECT_ID"
)
SCALE = 100
CRS = "EPSG:4326"
WORKLOAD_TAG = "geemu-nsw-dnbr-recovery"

NSW_ADMIN_COLLECTION = "FAO/GAUL/2015/level1"
COUNTRY_NAME = "Australia"
STATE_NAME = "New South Wales"

PRE_START = "2018-10-01"
PRE_END = "2019-03-31"
POST_START = "2020-02-01"
POST_END = "2020-05-31"
RECOVERY_START = "2023-10-01"
RECOVERY_END = "2024-03-31"

DNBR_BURN_THRESHOLD = 0.20
EXPORT_FOLDER = "GEEMu_nsw_dnbr_recovery"
EXPORT_BURNED = "nsw_dnbr_burned_area_100m"
EXPORT_RECOVERY = "nsw_nbr_recovery_100m"


@dataclass(frozen=True)
class AnalysisWindows:
    pre_start: str
    pre_end: str
    post_start: str
    post_end: str
    recovery_start: str
    recovery_end: str


def analysis_windows() -> AnalysisWindows:
    return AnalysisWindows(
        pre_start=PRE_START,
        pre_end=PRE_END,
        post_start=POST_START,
        post_end=POST_END,
        recovery_start=RECOVERY_START,
        recovery_end=RECOVERY_END,
    )


def resolve_project(cli_project: str | None) -> str | None:
    project = cli_project or PROJECT_ID
    if project == "PROJECT_ID":
        return None
    return project


def require_project(cli_project: str | None) -> str:
    project = resolve_project(cli_project)
    if project:
        return project
    raise RuntimeError(
        "Cloud Project ID is required before any Earth Engine request. "
        "Run with --project PROJECT_ID or set EE_PROJECT/GOOGLE_CLOUD_PROJECT/EE_PROJECT_ID."
    )


def print_dry_run() -> None:
    windows = analysis_windows()
    print(f"case={CASE_NAME}")
    print(f"region={STATE_NAME}, {COUNTRY_NAME}")
    print(f"scale={SCALE}")
    print(f"crs={CRS}")
    print(f"admin_collection={NSW_ADMIN_COLLECTION}")
    print("project=PROJECT_ID (required before online Earth Engine run)")
    print(f"pre={windows.pre_start}/{windows.pre_end}")
    print(f"post={windows.post_start}/{windows.post_end}")
    print(f"recovery={windows.recovery_start}/{windows.recovery_end}")
    print(f"burn_threshold_dnbr={DNBR_BURN_THRESHOLD}")
    print("NBR = (NIR - SWIR2) / (NIR + SWIR2)")
    print("dNBR = pre_fire_NBR - post_fire_NBR")
    print("burned_area = dNBR >= burn_threshold_dnbr")
    print("NBR_recovery_ratio = (recovery_NBR - post_fire_NBR) / dNBR, masked to burned_area")


def initialize_ee(project: str) -> None:
    try:
        geemap.ee_initialize(project=project)
        ee.data.getAssetRoots()
    except Exception as exc:
        raise RuntimeError(
            "Earth Engine/geemap initialization failed. Check authentication, proxy, "
            "Cloud Project permissions, then retry with --project PROJECT_ID."
        ) from exc

    try:
        ee.data.setDefaultWorkloadTag(WORKLOAD_TAG)
    except Exception:
        pass


def nsw_roi():
    nsw = (
        ee.FeatureCollection(NSW_ADMIN_COLLECTION)
        .filter(ee.Filter.eq("ADM0_NAME", COUNTRY_NAME))
        .filter(ee.Filter.eq("ADM1_NAME", STATE_NAME))
    )
    return nsw.geometry()


def mask_landsat_c2_l2(image):
    qa_mask = image.select("QA_PIXEL").bitwiseAnd(int("11111", 2)).eq(0)
    saturation_mask = image.select("QA_RADSAT").eq(0)
    optical = image.select("SR_B.").multiply(0.0000275).add(-0.2)
    return (
        image.addBands(optical, None, True)
        .updateMask(qa_mask)
        .updateMask(saturation_mask)
        .copyProperties(image, image.propertyNames())
    )


def add_nbr(image):
    nbr = image.normalizedDifference(["SR_B5", "SR_B7"]).rename("NBR")
    return image.addBands(nbr)


def landsat_nbr_composite(region, start_date: str, end_date: str):
    l8 = ee.ImageCollection("LANDSAT/LC08/C02/T1_L2")
    l9 = ee.ImageCollection("LANDSAT/LC09/C02/T1_L2")
    collection = (
        l8.merge(l9)
        .filterBounds(region.bounds(maxError=1000))
        .filterDate(start_date, end_date)
        .map(mask_landsat_c2_l2)
        .map(add_nbr)
    )
    return collection.select("NBR").median().rename("NBR")


def build_products(roi):
    windows = analysis_windows()
    pre_nbr = landsat_nbr_composite(roi, windows.pre_start, windows.pre_end).rename("pre_NBR")
    post_nbr = landsat_nbr_composite(roi, windows.post_start, windows.post_end).rename("post_NBR")
    recovery_nbr = landsat_nbr_composite(
        roi, windows.recovery_start, windows.recovery_end
    ).rename("recovery_NBR")

    dnbr = pre_nbr.subtract(post_nbr).rename("dNBR")
    burned_mask = dnbr.gte(DNBR_BURN_THRESHOLD).selfMask().rename("burned_mask")

    nbr_recovery_change = recovery_nbr.subtract(post_nbr).updateMask(burned_mask).rename(
        "NBR_recovery_change"
    )
    nbr_recovery_ratio = (
        nbr_recovery_change.divide(dnbr)
        .updateMask(burned_mask)
        .clamp(0, 1.5)
        .rename("NBR_recovery_ratio")
    )
    nbr_recovery_class = (
        nbr_recovery_ratio.expression(
            "(r >= 0.8) ? 3 : (r >= 0.4) ? 2 : (r >= 0.1) ? 1 : 0",
            {"r": nbr_recovery_ratio},
        )
        .updateMask(burned_mask)
        .rename("NBR_recovery_class")
        .toByte()
    )

    return (
        pre_nbr.addBands(
            [
                post_nbr,
                recovery_nbr,
                dnbr,
                burned_mask,
                nbr_recovery_change,
                nbr_recovery_ratio,
                nbr_recovery_class,
            ]
        )
        .clip(roi)
    )


def compute_summary(products, roi) -> dict:
    area_ha = ee.Image.pixelArea().divide(10000)
    burned_area_ha = area_ha.updateMask(products.select("burned_mask")).rename("burned_area_ha")

    summary_image = products.select(["dNBR", "NBR_recovery_ratio"]).addBands(burned_area_ha)
    summary = summary_image.reduceRegion(
        reducer=ee.Reducer.mean().combine(ee.Reducer.sum(), sharedInputs=True),
        geometry=roi,
        scale=SCALE,
        crs=CRS,
        maxPixels=1e13,
        tileScale=8,
        bestEffort=True,
    )
    recovery_hist = products.select("NBR_recovery_class").reduceRegion(
        reducer=ee.Reducer.frequencyHistogram(),
        geometry=roi,
        scale=SCALE,
        crs=CRS,
        maxPixels=1e13,
        tileScale=8,
        bestEffort=True,
    )

    return {
        "case": CASE_NAME,
        "region": f"{STATE_NAME}, {COUNTRY_NAME}",
        "scale": SCALE,
        "windows": asdict(analysis_windows()),
        "burn_threshold_dnbr": DNBR_BURN_THRESHOLD,
        "summary": summary.getInfo(),
        "recovery_class_histogram": recovery_hist.getInfo(),
    }


def harmonize_export_dtype(image):
    """Cast every band to Float32 so a multi-band export does not fail with
    "Exported bands must have compatible data types; found inconsistent types:
    Float32 and Byte". That error appears when float index bands (dNBR,
    NBR_recovery_ratio) are mixed with Byte bands from .gte()/.selfMask()/
    .toByte() (burned_mask, NBR_recovery_class). To keep a categorical band as
    Byte instead, export it in its own task.
    """
    return image.toFloat()


def export_images(products, roi) -> list:
    burned_task = ee.batch.Export.image.toDrive(
        image=harmonize_export_dtype(
            products.select(["dNBR", "burned_mask", "pre_NBR", "post_NBR"])
        ),
        description=EXPORT_BURNED,
        folder=EXPORT_FOLDER,
        fileNamePrefix=EXPORT_BURNED,
        region=roi,
        scale=SCALE,
        crs=CRS,
        maxPixels=1e13,
    )
    recovery_task = ee.batch.Export.image.toDrive(
        image=harmonize_export_dtype(
            products.select(
                [
                    "recovery_NBR",
                    "NBR_recovery_change",
                    "NBR_recovery_ratio",
                    "NBR_recovery_class",
                ]
            )
        ),
        description=EXPORT_RECOVERY,
        folder=EXPORT_FOLDER,
        fileNamePrefix=EXPORT_RECOVERY,
        region=roi,
        scale=SCALE,
        crs=CRS,
        maxPixels=1e13,
    )

    tasks = []
    for task in [burned_task, recovery_task]:
        task.start()
        tasks.append(task)
    return tasks


def build_map(products, roi):
    m = geemap.Map()
    m.centerObject(roi, 6)
    m.addLayer(products.select("dNBR"), {"min": -0.2, "max": 0.8, "palette": ["green", "white", "orange", "red"]}, "dNBR")
    m.addLayer(products.select("burned_mask"), {"palette": ["red"]}, "dNBR burned area")
    m.addLayer(products.select("NBR_recovery_ratio"), {"min": 0, "max": 1, "palette": ["red", "yellow", "green"]}, "NBR recovery ratio")
    return m


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="NSW dNBR burned-area detection and NBR recovery example.")
    parser.add_argument("--dry-run", action="store_true", help="Print parameters without Earth Engine auth.")
    parser.add_argument("--project", help="Earth Engine Cloud Project ID.")
    parser.add_argument("--export", action="store_true", help="Start Drive exports after summary computation.")
    parser.add_argument("--no-map", action="store_true", help="Skip geemap preview.")
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

    roi = nsw_roi()
    products = build_products(roi)
    summary = compute_summary(products, roi)
    print(json.dumps(summary, ensure_ascii=False, indent=2))

    if args.export:
        tasks = export_images(products, roi)
        print(f"Started {len(tasks)} export tasks:")
        for task in tasks:
            print(task.id)

    if not args.no_map:
        build_map(products, roi)

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
