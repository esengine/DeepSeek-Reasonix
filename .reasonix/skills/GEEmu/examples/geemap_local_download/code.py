from __future__ import annotations

import argparse
import os
import sys
import time
from pathlib import Path


SCRIPT_DIR = Path(__file__).resolve().parent
if sys.path and Path(sys.path[0]).resolve() == SCRIPT_DIR:
    sys.path.pop(0)


DEFAULT_PROJECT_ID = (
    os.environ.get("EE_PROJECT")
    or os.environ.get("GOOGLE_CLOUD_PROJECT")
    or os.environ.get("EE_PROJECT_ID")
    or "PROJECT_ID"
)
DEFAULT_DATASET_ID = "ESA/WorldCover/v100"
DEFAULT_BAND_NAME = "Map"
DEFAULT_ROI_ASSET = "projects/PROJECT_ID/assets/ROI_ASSET"
DEFAULT_OUTPUT_DIR = SCRIPT_DIR / "out" / "ESA2020"
DEFAULT_OUTPUT_FILENAME = "ESA_WorldCover_2020_landcover.tif"
DEFAULT_SCALE = 10
WORKLOAD_TAG = "geemu-local-download"


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Download an Earth Engine image directly to a local GeoTIFF with geemap."
    )
    parser.add_argument("--project", default=DEFAULT_PROJECT_ID, help="Google Cloud Project ID.")
    parser.add_argument(
        "--roi-asset",
        default=DEFAULT_ROI_ASSET,
        help="Earth Engine FeatureCollection asset used as the download region.",
    )
    parser.add_argument(
        "--bbox",
        nargs=4,
        type=float,
        metavar=("WEST", "SOUTH", "EAST", "NORTH"),
        help="Optional lon/lat rectangle. If set, it overrides --roi-asset.",
    )
    parser.add_argument("--dataset", default=DEFAULT_DATASET_ID, help="Earth Engine ImageCollection ID.")
    parser.add_argument("--band", default=DEFAULT_BAND_NAME, help="Band to download.")
    parser.add_argument("--scale", type=float, default=DEFAULT_SCALE, help="Download scale in meters.")
    parser.add_argument("--crs", default=None, help="Optional output CRS, such as EPSG:4326.")
    parser.add_argument("--output-dir", type=Path, default=DEFAULT_OUTPUT_DIR, help="Local output folder.")
    parser.add_argument("--filename", default=DEFAULT_OUTPUT_FILENAME, help="Output GeoTIFF filename.")
    parser.add_argument("--overwrite", action="store_true", help="Overwrite an existing local file.")
    parser.add_argument("--inspect", action="store_true", help="Inspect image metadata before downloading.")
    parser.add_argument("--dry-run", action="store_true", help="Print the plan without contacting Earth Engine.")
    return parser.parse_args()


def require_real_project(project: str) -> None:
    if project == "PROJECT_ID":
        raise RuntimeError(
            "Cloud Project ID is required before any Earth Engine request. "
            "Set EE_PROJECT/GOOGLE_CLOUD_PROJECT/EE_PROJECT_ID or pass --project."
        )


def initialize_ee(ee, geemap, project: str) -> None:
    require_real_project(project)
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


def output_path(args: argparse.Namespace) -> Path:
    return args.output_dir / args.filename


def build_region(ee, args: argparse.Namespace):
    if args.bbox:
        west, south, east, north = args.bbox
        return ee.Geometry.Rectangle([west, south, east, north], proj="EPSG:4326", geodesic=False)

    if args.roi_asset == DEFAULT_ROI_ASSET:
        raise RuntimeError(
            "A real ROI is required before online download. Pass --roi-asset or --bbox."
        )

    return ee.FeatureCollection(args.roi_asset).geometry()


def build_image(ee, args: argparse.Namespace):
    return ee.ImageCollection(args.dataset).first().select(args.band).toByte()


def print_plan(args: argparse.Namespace) -> None:
    print("case=geemap_local_download")
    print("method=geemap.download_ee_image")
    print(f"project={args.project}")
    print(f"dataset={args.dataset}")
    print(f"band={args.band}")
    print(f"scale={args.scale}")
    print(f"crs={args.crs or 'native'}")
    print(f"roi_asset={args.roi_asset}")
    print(f"bbox={args.bbox or 'not set'}")
    print(f"output={output_path(args)}")
    print("resampling=near")
    print("dtype=uint8")
    print("boundary_note=Use direct local download only for small or controlled regions.")


def inspect_image(image) -> None:
    print(f"Bands: {image.bandNames().getInfo()}")
    print(f"Projection: {image.projection().getInfo()}")


def download(args: argparse.Namespace) -> None:
    import ee
    import geemap

    initialize_ee(ee, geemap, args.project)
    args.output_dir.mkdir(parents=True, exist_ok=True)

    target = output_path(args)
    if target.exists() and not args.overwrite:
        raise RuntimeError(f"Output already exists: {target}. Use --overwrite to replace it.")

    region = build_region(ee, args)
    image = build_image(ee, args).clip(region)

    if args.inspect:
        inspect_image(image)

    kwargs = {
        "image": image,
        "filename": str(target),
        "region": region,
        "scale": args.scale,
        "resampling": "near",
        "dtype": "uint8",
        "overwrite": args.overwrite,
    }
    if args.crs:
        kwargs["crs"] = args.crs

    started = time.time()
    geemap.download_ee_image(**kwargs)
    elapsed_minutes = (time.time() - started) / 60

    print(f"Saved to: {target}")
    print(f"Elapsed minutes: {elapsed_minutes:.1f}")
    if target.exists():
        print(f"File size MB: {target.stat().st_size / (1024 * 1024):.1f}")


def main() -> int:
    args = parse_args()
    print_plan(args)

    if args.dry_run:
        return 0

    try:
        download(args)
    except RuntimeError as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 2

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
