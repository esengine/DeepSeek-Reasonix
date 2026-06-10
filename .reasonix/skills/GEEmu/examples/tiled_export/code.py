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
# Optional pre-made grid FeatureCollection. Leave empty to generate a grid from
# the ROI with ee.Geometry.coveringGrid (no uploaded asset required).
TILE_ASSET = ""
GRID_CRS = "EPSG:3857"   # meter-based projection so tiles are ~square
TILE_SIZE_M = 50000      # tile edge length in meters (~50 km)
START_DATE = "2024-06-01"
END_DATE = "2024-09-30"
EXPORT_FOLDER = "GEEMu_tiled_exports"
EXPORT_PREFIX = "s2_ndvi_tile"
SCALE = 10
CRS = "EPSG:4326"
MAX_TILES = 20
WORKLOAD_TAG = "geemu-tiled-export"


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
    return image.addBands(image.normalizedDifference(["B8", "B4"]).rename("NDVI"))


def build_image(roi: ee.FeatureCollection) -> ee.Image:
    search_region = roi.geometry().bounds(maxError=100)
    collection = (
        ee.ImageCollection("COPERNICUS/S2_SR_HARMONIZED")
        .filterBounds(search_region)
        .filterDate(START_DATE, END_DATE)
        .select(["B4", "B8", "SCL"])
        .map(mask_s2_scl)
        .map(add_ndvi)
    )
    return collection.select("NDVI").median().rename("NDVI")


def get_tiles(roi: ee.FeatureCollection) -> ee.FeatureCollection:
    """Return the tile grid covering the ROI.

    If TILE_ASSET is set, load that grid; otherwise generate one with
    ee.Geometry.coveringGrid so the example works without a pre-uploaded asset.
    coveringGrid builds square cells of TILE_SIZE_M in GRID_CRS (a meter-based
    projection); filterBounds keeps only cells that touch the ROI.
    """
    geom = roi.geometry()
    if TILE_ASSET:
        return ee.FeatureCollection(TILE_ASSET).filterBounds(geom.bounds(maxError=100))
    return geom.coveringGrid(GRID_CRS, TILE_SIZE_M).filterBounds(geom)


def harmonize_export_dtype(image: ee.Image) -> ee.Image:
    """Cast every band to Float32 before export so a multi-band export does not
    fail with "Exported bands must have compatible data types; found
    inconsistent types: Float32 and Byte" (float index bands mixed with Byte
    mask/class bands). Export categorical Byte products in a separate task to
    keep their integer dtype.
    """
    return image.toFloat()


def start_tiled_exports(image: ee.Image, roi: ee.FeatureCollection) -> list[ee.batch.Task]:
    tiles = get_tiles(roi)
    tile_count = int(tiles.size().getInfo())

    if tile_count == 0:
        raise ValueError("No tiles intersect the ROI. Check ROI_ASSET, GRID_CRS, and TILE_SIZE_M.")
    if tile_count > MAX_TILES:
        raise ValueError(
            f"Tile count is {tile_count}, above MAX_TILES={MAX_TILES}. "
            "Increase MAX_TILES (or TILE_SIZE_M) only after checking task quota and tile safety."
        )

    export_image = harmonize_export_dtype(image)
    tasks: list[ee.batch.Task] = []
    tile_list = tiles.toList(tile_count)
    for index in range(tile_count):
        region = ee.Feature(tile_list.get(index)).geometry()
        task = ee.batch.Export.image.toDrive(
            image=export_image,
            description=f"{EXPORT_PREFIX}_{index:03d}",
            folder=EXPORT_FOLDER,
            fileNamePrefix=f"{EXPORT_PREFIX}_{index:03d}",
            region=region,
            scale=SCALE,
            crs=CRS,
            maxPixels=1e13,
        )
        task.start()
        tasks.append(task)
        print(f"Started tile {index:03d}: {task.id}")

    return tasks


def print_dry_run() -> None:
    grid_mode = f"asset {TILE_ASSET}" if TILE_ASSET else f"generated coveringGrid {TILE_SIZE_M} m in {GRID_CRS}"
    print(f"case={EXPORT_PREFIX}")
    print(f"roi_asset={ROI_ASSET}")
    print("project=PROJECT_ID (required before online Earth Engine run)")
    print(f"dataset=COPERNICUS/S2_SR_HARMONIZED  time={START_DATE}/{END_DATE}")
    print(f"scale={SCALE}  crs={CRS}")
    print(f"grid={grid_mode}  max_tiles={MAX_TILES}")
    print("NDVI = (B8 - B4) / (B8 + B4) -> median composite -> per-tile export")
    print(f"export=Drive folder '{EXPORT_FOLDER}' (only with --export)")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Tiled Sentinel-2 NDVI export example.")
    parser.add_argument("--dry-run", action="store_true", help="Print parameters without Earth Engine auth.")
    parser.add_argument("--project", help="Earth Engine Cloud Project ID.")
    parser.add_argument("--export", action="store_true", help="Start the per-tile Drive export tasks.")
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
    image = build_image(roi)

    if args.export:
        tasks = start_tiled_exports(image, roi)
        print(f"Started {len(tasks)} export tasks.")
    else:
        tiles = get_tiles(roi)
        print(f"Prepared {int(tiles.size().getInfo())} tiles. Re-run with --export to start tasks.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
