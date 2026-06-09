"""geo_env_status — 探测 GDAL / QGIS / GEE 遥感环境状态。

GDAL 探测：检查关键二进制工具是否存在（gdalinfo, ogrinfo 等），四态分级。
QGIS 探测：检查 QGIS 安装路径 + Python 子进程探活。
GEE 探测：检查 earthengine-api 安装 + 认证 + Initialize。
"""

import json
import logging
import os
import shutil
import subprocess
import sys
from pathlib import Path

logger = logging.getLogger(__name__)

# ── GDAL 探测 ─────────────────────────────────────────────────────

_REQUIRED_GDAL_RASTER = ["gdalinfo", "gdalwarp", "gdal_translate", "gdaldem"]
_REQUIRED_GDAL_VECTOR = ["ogrinfo", "ogr2ogr"]


def _detect_gdal_bin() -> str | None:
    """探测 GDAL bin 目录。优先从当前 Python 环境的 conda Library/bin。"""
    exe_dir = Path(sys.executable).parent
    conda_bin = exe_dir / "Library" / "bin"
    if conda_bin.is_dir():
        return str(conda_bin)

    found = shutil.which("gdalinfo")
    if found:
        return str(Path(found).parent)
    return None


def _check_tool(bin_dir: str, tool: str) -> bool:
    """检查指定工具是否存在于 bin_dir。"""
    if os.name == "nt":
        names = [f"{tool}.exe", tool]
    else:
        names = [tool]
    for name in names:
        if (Path(bin_dir) / name).exists():
            return True
    return shutil.which(tool, path=bin_dir) is not None


def probe_gdal() -> dict:
    """探测 GDAL 环境。返回四态分级结果。"""
    bin_dir = _detect_gdal_bin()

    if not bin_dir:
        return {
            "status": "bad",
            "reason": "GDAL bin 目录未找到。请安装 GDAL: conda install -c conda-forge gdal",
            "bin_dir": None,
            "details": {},
        }

    raster_ok = all(_check_tool(bin_dir, tool) for tool in _REQUIRED_GDAL_RASTER)
    vector_ok = all(_check_tool(bin_dir, tool) for tool in _REQUIRED_GDAL_VECTOR)

    details = {}
    for tool in _REQUIRED_GDAL_RASTER + _REQUIRED_GDAL_VECTOR:
        details[tool] = _check_tool(bin_dir, tool)

    # Python binding 版本
    try:
        from osgeo import gdal
        gdal_version = gdal.__version__
    except ImportError:
        gdal_version = None

    if raster_ok and vector_ok:
        return {
            "status": "ready",
            "bin_dir": bin_dir,
            "gdal_version": gdal_version,
            "details": details,
        }
    elif raster_ok:
        return {
            "status": "raster-only",
            "reason": "矢量工具 (ogrinfo, ogr2ogr) 缺失。conda install -c conda-forge gdal 重装可能修复。",
            "bin_dir": bin_dir,
            "gdal_version": gdal_version,
            "details": details,
        }
    elif vector_ok:
        return {
            "status": "vector-only",
            "reason": "栅格工具 (gdalinfo, gdalwarp 等) 缺失。conda install -c conda-forge gdal 重装可能修复。",
            "bin_dir": bin_dir,
            "gdal_version": gdal_version,
            "details": details,
        }
    else:
        missing = [t for t, ok in details.items() if not ok]
        return {
            "status": "bad",
            "reason": f"关键工具缺失: {', '.join(missing)}",
            "bin_dir": bin_dir,
            "gdal_version": gdal_version,
            "details": details,
        }


# ── QGIS 探测 ─────────────────────────────────────────────────────

_QGIS_HINTS_WIN = [
    r"C:\Program Files\QGIS 3.40.4",
    r"C:\Program Files\QGIS 3.38.4",
    r"C:\Program Files\QGIS 3.34.4",
    r"C:\OSGeo4W",
]


def _find_qgis_python() -> str | None:
    """查找 QGIS 自带的 Python 解释器路径。"""
    if os.name == "nt":
        for hint in _QGIS_HINTS_WIN:
            for sub in ["bin", "apps/Python312", "apps/Python311", "apps/Python310"]:
                py = Path(hint) / sub / "python3.exe"
                if py.exists():
                    return str(py)
            bat = Path(hint) / "bin" / "python-qgis-ltr.bat"
            if bat.exists():
                return str(bat)
    else:
        for name in ["python3", "qgis"]:
            p = shutil.which(name)
            if p:
                return p
    return None


def _probe_qgis_subprocess(qgis_python: str) -> dict:
    """通过子进程探测 QGIS Python 环境。30s 超时。"""
    probe_script = """
import json, sys
try:
    from osgeo import gdal
    gdal_ver = gdal.__version__
except Exception as e:
    gdal_ver = None
    sys.stderr.write(f"gdal import failed: {e}\\n")

try:
    from qgis.core import QgsApplication
    QgsApplication.setAttribute(
        getattr(QgsApplication, 'AA_EnableHighDpiScaling', 0), False
    )
    app = QgsApplication([], False)
    app.initQgis()
    qgis_ver = app.platform()
    app.exitQgis()
except Exception as e:
    qgis_ver = None
    sys.stderr.write(f"qgis import/init failed: {e}\\n")

try:
    import processing
    processing_ready = True
except Exception:
    processing_ready = False

print(json.dumps({
    "gdal_version": gdal_ver,
    "qgis_version": qgis_ver,
    "processing_ready": processing_ready,
}))
"""
    try:
        proc = subprocess.run(
            [qgis_python, "-c", probe_script],
            capture_output=True, text=True, timeout=30,
            env={**os.environ, "PYTHONUTF8": "1", "QGIS_DEBUG": "0"},
        )
        if proc.returncode == 0:
            # 解析最后一行 JSON
            for line in reversed(proc.stdout.strip().split("\n")):
                try:
                    return json.loads(line)
                except json.JSONDecodeError:
                    continue
        return {"error": f"exit code {proc.returncode}", "stderr": proc.stderr[:500]}
    except subprocess.TimeoutExpired:
        return {"error": "QGIS Python 探活超时 (30s)"}
    except Exception as e:
        return {"error": str(e)}


def probe_qgis() -> dict:
    """探测 QGIS 环境。"""
    qgis_python = _find_qgis_python()

    if not qgis_python:
        return {
            "status": "not-installed",
            "reason": (
                "未找到 QGIS 安装。请从 https://qgis.org 下载安装 QGIS，"
                "安装后 geo_env_status 会自动探测。"
            ),
            "qgis_python": None,
        }

    result = _probe_qgis_subprocess(qgis_python)
    if "error" in result:
        return {
            "status": "bad",
            "reason": result["error"],
            "qgis_python": qgis_python,
        }

    return {
        "status": "ready" if result.get("processing_ready") else "bad",
        "qgis_python": qgis_python,
        "gdal_version": result.get("gdal_version"),
        "qgis_version": result.get("qgis_version"),
        "processing_ready": result.get("processing_ready", False),
    }


# ── GEE 探测 ──────────────────────────────────────────────────────

def probe_gee() -> dict:
    """探测 GEE 环境。检查安装、认证、Initialize。"""
    try:
        import ee
    except ImportError:
        return {
            "status": "not-installed",
            "reason": "earthengine-api 未安装。pip install earthengine-api geemap",
            "ee_version": None,
        }

    ee_version = ee.__version__

    # 检查认证状态
    credentials_path = None
    try:
        from ee import oauth
        credentials_path = oauth.get_credentials_path()
    except Exception:
        pass

    has_credentials = bool(credentials_path and os.path.exists(credentials_path))

    if not has_credentials:
        return {
            "status": "auth-required",
            "reason": "GEE 未认证。在终端执行: earthengine authenticate",
            "ee_version": ee_version,
        }

    # 尝试 Initialize
    project = os.environ.get("GEE_PROJECT")
    try:
        ee.Initialize(project=project)
        return {
            "status": "ready",
            "ee_version": ee_version,
            "project": project,
        }
    except Exception as e:
        msg = str(e).lower()
        if "auth" in msg or "credential" in msg or "token" in msg:
            return {
                "status": "auth-required",
                "reason": f"GEE 认证已过期或无效: {e}",
                "ee_version": ee_version,
            }
        return {
            "status": "init-failed",
            "reason": f"GEE Initialize 失败: {e}",
            "ee_version": ee_version,
            "project": project,
        }


# ── 统一入口 ──────────────────────────────────────────────────────

def _summary(s):
    return s.get("status", "unknown")


def run(args: dict) -> tuple[str, bool]:
    gdal = probe_gdal()
    qgis = probe_qgis()
    gee = probe_gee()

    diagnostic = {
        "gdal": {k: v for k, v in gdal.items() if k != "details"},
        "qgis": qgis,
        "gee": gee,
    }
    logger.info("env probe: %s", json.dumps(diagnostic, ensure_ascii=False))

    lines = [
        "═" * 42,
        "  遥感环境状态诊断",
        "═" * 42,
        "",
        f"  GDAL   [{_summary(gdal):12s}]  {gdal.get('gdal_version') or ''}",
        f"  QGIS   [{_summary(qgis):12s}]  {qgis.get('qgis_version') or ''}",
        f"  GEE    [{_summary(gee):12s}]  {gee.get('ee_version') or ''}",
        "",
        "── GDAL ──",
        f"  状态: {gdal['status']}",
    ]
    if gdal.get("bin_dir"):
        lines.append(f"  路径: {gdal['bin_dir']}")
    if gdal.get("gdal_version"):
        lines.append(f"  版本: {gdal['gdal_version']}")
    if gdal.get("reason"):
        lines.append(f"  说明: {gdal['reason']}")

    lines.append("")
    lines.append("── QGIS ──")
    lines.append(f"  状态: {qgis['status']}")
    if qgis.get("qgis_python"):
        lines.append(f"  Python: {qgis['qgis_python']}")
    if qgis.get("reason"):
        lines.append(f"  说明: {qgis['reason']}")

    lines.append("")
    lines.append("── GEE ──")
    lines.append(f"  状态: {gee['status']}")
    if gee.get("ee_version"):
        lines.append(f"  版本: {gee['ee_version']}")
    if gee.get("project"):
        lines.append(f"  项目: {gee['project']}")
    if gee.get("reason"):
        lines.append(f"  说明: {gee['reason']}")

    # 修复指引
    issues = []
    if gdal["status"] not in ("ready", "raster-only", "vector-only"):
        issues.append("GDAL: conda install -c conda-forge gdal")
    if qgis["status"] != "ready":
        issues.append("QGIS: 从 https://qgis.org 下载安装")
    if gee["status"] == "not-installed":
        issues.append("GEE: pip install earthengine-api")
    elif gee["status"] == "auth-required":
        issues.append("GEE: 在终端执行 earthengine authenticate")

    if issues:
        lines.append("")
        lines.append("── 修复指引 ──")
        for i, fix in enumerate(issues, 1):
            lines.append(f"  {i}. {fix}")

    lines.append("")
    lines.append("═" * 42)

    return "\n".join(lines), False
