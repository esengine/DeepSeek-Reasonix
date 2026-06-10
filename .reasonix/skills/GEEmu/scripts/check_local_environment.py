from __future__ import annotations

import argparse
import importlib
import importlib.metadata
import json
import os
import platform
import sys
from pathlib import Path
from typing import Any


def import_status(module_name: str, package_name: str | None = None) -> dict[str, Any]:
    status: dict[str, Any] = {"installed": False, "version": None, "error": None}
    try:
        module = importlib.import_module(module_name)
        status["installed"] = True
        status["version"] = getattr(module, "__version__", None)
        if status["version"] is None and package_name:
            try:
                status["version"] = importlib.metadata.version(package_name)
            except importlib.metadata.PackageNotFoundError:
                status["version"] = None
    except Exception as exc:
        status["error"] = f"{type(exc).__name__}: {exc}"
    return status


def earthengine_credentials() -> dict[str, Any]:
    candidates = [
        Path.home() / ".config" / "earthengine" / "credentials",
    ]
    existing = [str(path) for path in candidates if path.exists()]
    return {
        "found": bool(existing),
        "paths": existing,
        "checked": [str(path) for path in candidates],
    }


def cloud_project(cli_project: str | None) -> dict[str, Any]:
    if cli_project and cli_project != "PROJECT_ID":
        return {"configured": True, "value": cli_project, "source": "--project"}

    for name in ["EE_PROJECT", "GOOGLE_CLOUD_PROJECT", "EE_PROJECT_ID"]:
        value = os.environ.get(name)
        if value and value != "PROJECT_ID":
            return {"configured": True, "value": value, "source": name}

    return {
        "configured": False,
        "value": None,
        "source": None,
        "hint": "Provide --project PROJECT_ID or set EE_PROJECT/GOOGLE_CLOUD_PROJECT/EE_PROJECT_ID.",
    }


def geemap_helpers() -> dict[str, Any]:
    try:
        geemap = importlib.import_module("geemap")
    except Exception as exc:
        return {"available": False, "error": f"{type(exc).__name__}: {exc}", "helpers": {}}

    helpers = {
        "shp_to_ee": hasattr(geemap, "shp_to_ee"),
        "geojson_to_ee": hasattr(geemap, "geojson_to_ee"),
        "gdf_to_ee": hasattr(geemap, "gdf_to_ee"),
    }
    return {"available": all(helpers.values()), "error": None, "helpers": helpers}


def ee_online_check(project: str | None) -> dict[str, Any]:
    result: dict[str, Any] = {
        "attempted": True,
        "initialized": False,
        "asset_roots_count": None,
        "error": None,
    }
    try:
        ee = importlib.import_module("ee")
        if project:
            ee.Initialize(project=project)
        else:
            ee.Initialize()
        roots = ee.data.getAssetRoots()
        result["initialized"] = True
        result["asset_roots_count"] = len(roots)
    except Exception as exc:
        result["error"] = f"{type(exc).__name__}: {exc}"
    return result


def build_report(project: str | None, skip_ee_network: bool) -> dict[str, Any]:
    project_status = cloud_project(project)
    report = {
        "python": {
            "executable": sys.executable,
            "version": sys.version,
            "platform": platform.platform(),
        },
        "imports": {
            "ee": import_status("ee", "earthengine-api"),
            "geemap": import_status("geemap", "geemap"),
            "geopandas": import_status("geopandas", "geopandas"),
        },
        "cloud_project": project_status,
        "earthengine_credentials": earthengine_credentials(),
        "geemap_vector_helpers": geemap_helpers(),
        "ee_online_check": {
            "attempted": False,
            "initialized": None,
            "asset_roots_count": None,
            "error": "skipped",
        },
    }
    if not skip_ee_network:
        report["ee_online_check"] = ee_online_check(project_status["value"])
    return report


def print_text(report: dict[str, Any]) -> None:
    print("GEEMu local environment check")
    print(f"Python: {report['python']['executable']}")
    print(f"Version: {report['python']['version'].splitlines()[0]}")
    print()
    for name, status in report["imports"].items():
        marker = "OK" if status["installed"] else "MISSING"
        version = status["version"] or "unknown"
        print(f"{name}: {marker} ({version})")
        if status["error"]:
            print(f"  error: {status['error']}")
    print()
    creds = report["earthengine_credentials"]
    print(f"Earth Engine credentials file: {'FOUND' if creds['found'] else 'NOT FOUND'}")
    for path in creds["paths"]:
        print(f"  {path}")
    project = report["cloud_project"]
    print(f"Cloud Project: {'OK' if project['configured'] else 'MISSING'}")
    if project["configured"]:
        print(f"  source: {project['source']}")
        print(f"  value: {project['value']}")
    else:
        print(f"  hint: {project['hint']}")
    helpers = report["geemap_vector_helpers"]
    print(f"geemap local vector helpers: {'OK' if helpers['available'] else 'CHECK'}")
    for name, value in helpers.get("helpers", {}).items():
        print(f"  {name}: {value}")
    ee_check = report["ee_online_check"]
    print()
    if ee_check["attempted"]:
        print(f"Earth Engine online initialize: {'OK' if ee_check['initialized'] else 'FAILED'}")
        if ee_check["error"]:
            print(f"  error: {ee_check['error']}")
    else:
        print("Earth Engine online initialize: SKIPPED")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Check local GEEMu Python/geemap/Earth Engine environment.")
    parser.add_argument("--project", help="Earth Engine Cloud Project ID for online initialize check.")
    parser.add_argument("--skip-ee-network", action="store_true", help="Skip ee.Initialize() and only check imports/credential files.")
    parser.add_argument("--json", action="store_true", help="Print JSON report.")
    parser.add_argument("--require-project", action="store_true", help="Exit non-zero if no Cloud Project ID is configured.")
    parser.add_argument("--require-auth", action="store_true", help="Exit non-zero if Earth Engine online check fails.")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    report = build_report(project=args.project, skip_ee_network=args.skip_ee_network)
    if args.json:
        print(json.dumps(report, ensure_ascii=False, indent=2))
    else:
        print_text(report)

    if args.require_project and not report["cloud_project"]["configured"]:
        return 3
    if args.require_auth and not report["ee_online_check"]["initialized"]:
        return 2
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
