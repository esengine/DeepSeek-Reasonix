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

def _scan_qgis_roots() -> list[str]:
    """扫描系统中所有 QGIS 安装根目录。参照 GeoCode findQgisRootFromHint 逻辑。"""
    roots = []
    if os.name == "nt":
        search_dirs = [r"C:\Program Files", r"C:\Program Files (x86)", "C:\\"]
        for base in search_dirs:
            base_path = Path(base)
            if not base_path.is_dir():
                continue
            try:
                for entry in base_path.iterdir():
                    if not entry.is_dir():
                        continue
                    name = entry.name.lower()
                    if "qgis" in name or "osgeo" in name:
                        # 验证是 QGIS 安装根（有 bin 子目录）
                        if (entry / "bin" / "python3.exe").exists() or \
                           (entry / "bin" / "python-qgis-ltr.bat").exists() or \
                           (entry / "bin" / "python-qgis.bat").exists():
                            roots.append(str(entry))
            except PermissionError:
                continue
    else:
        for p in ["/usr", "/usr/local", "/Applications"]:
            base_path = Path(p)
            if not base_path.is_dir():
                continue
            try:
                for entry in base_path.iterdir():
                    if entry.is_dir() and "qgis" in entry.name.lower():
                        roots.append(str(entry))
            except PermissionError:
                continue
    return roots


def _find_qgis_python() -> tuple[str | None, str | None, str | None]:
    """查找 QGIS Python 解释器。返回 (python_path, qgis_root, bat_path)。

    优先用 python-qgis-ltr.bat（自动设置完整 DLL 环境），
    回退到 bin/python3.exe + 手动 env 构建。
    """
    roots = _scan_qgis_roots()

    for root in roots:
        # 最优: python-qgis-ltr.bat — 自动处理所有 env
        for bat_name in ["python-qgis-ltr.bat", "python-qgis.bat"]:
            bat = Path(root) / "bin" / bat_name
            if bat.exists():
                # 同时找对应 python3.exe 作为直接调用回退
                py = Path(root) / "bin" / "python3.exe"
                return (str(py) if py.exists() else str(bat)), root, str(bat)

        # 备选: bin/python3.exe
        py = Path(root) / "bin" / "python3.exe"
        if py.exists():
            return str(py), root, None

        # 旧版: apps/PythonNNN/python.exe
        apps_dir = Path(root) / "apps"
        if apps_dir.is_dir():
            import re
            try:
                for entry in sorted(apps_dir.iterdir(), reverse=True):
                    if re.match(r"^Python\d+$", entry.name, re.IGNORECASE):
                        py = entry / "python.exe"
                        if py.exists():
                            return str(py), root, None
            except PermissionError:
                pass

    return None, None, None


def _build_qgis_env(qgis_root: str) -> dict:
    """构建 QGIS Python 所需的干净环境变量。

    QGIS standalone 的 python3.exe 需要 PYTHONHOME + DLL PATH 配置。
    关键：必须避免 conda 环境的 GDAL/PROJ DLL 污染，否则 qgis_core.dll 加载崩溃。
    """
    # 从干净的系统环境起步，不继承 conda 的 PATH
    env = {}
    # 保留必要的系统变量
    for k in ("SYSTEMROOT", "WINDIR", "TEMP", "TMP", "USERPROFILE",
              "COMSPEC", "PATHEXT", "PROCESSOR_ARCHITECTURE",
              "NUMBER_OF_PROCESSORS", "HOMEDRIVE", "HOMEPATH"):
        if k in os.environ:
            env[k] = os.environ[k]

    apps_dir = Path(qgis_root) / "apps"
    python_home = None
    if apps_dir.is_dir():
        import re
        for entry in sorted(apps_dir.iterdir(), reverse=True):
            if re.match(r"^Python\d+$", entry.name, re.IGNORECASE):
                python_home = str(entry)
                break
    if python_home:
        env["PYTHONHOME"] = python_home

    # QGIS prefix
    qgis_prefix = None
    for sub in ["qgis-ltr", "qgis", "qgis-dev"]:
        candidate = apps_dir / sub
        if candidate.is_dir():
            qgis_prefix = str(candidate)
            break

    env["QGIS_PREFIX_PATH"] = qgis_prefix or str(qgis_root)
    env["QGIS_DEBUG"] = "0"
    env["PYTHONUTF8"] = "1"

    # PYTHONPATH
    python_paths = []
    if qgis_prefix:
        python_paths.append(str(Path(qgis_prefix) / "python"))
        python_paths.append(str(Path(qgis_prefix) / "python" / "plugins"))
    env["PYTHONPATH"] = os.pathsep.join(python_paths) if python_paths else ""

    # PATH: QGIS DLL 优先，再追加系统 PATH（不含 conda）
    qgis_paths = [
        str(Path(qgis_root) / "bin"),
        str(Path(qgis_root) / "apps" / "Qt5" / "bin"),
    ]
    if qgis_prefix:
        qgis_paths.append(str(Path(qgis_prefix) / "bin"))
    system_path = os.environ.get("PATH", "")
    # 过滤掉 conda 相关的路径，避免 DLL 冲突
    filtered_system = os.pathsep.join(
        p for p in system_path.split(os.pathsep)
        if "conda" not in p.lower() and "miniconda" not in p.lower()
    )
    env["PATH"] = os.pathsep.join(qgis_paths + [filtered_system])

    return env


def _probe_qgis_subprocess(qgis_python: str, qgis_root: str) -> dict:
    """通过子进程探测 QGIS Python 环境。30s 超时。"""
    probe_script = """
import json, math, os, sys, traceback
info = {"pythonVersion": ".".join(map(str, sys.version_info[:3]))}

# 1. GDAL
try:
    from osgeo import gdal, osr
    info["gdalVersion"] = gdal.__version__
except Exception as e:
    info["gdalVersion"] = None
    info["gdalError"] = str(e)
    print(json.dumps(info))
    sys.exit(0)

# 1.5 PROJ custom CRS transform — 验证 PROJ 库是否完整
try:
    src = osr.SpatialReference()
    src.ImportFromProj4("+proj=lcc +lat_1=25 +lat_2=47 +lat_0=0 +lon_0=105 +x_0=0 +y_0=0 +datum=WGS84 +units=m +no_defs")
    src.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER)
    dst = osr.SpatialReference()
    dst.ImportFromEPSG(4326)
    dst.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER)
    transform = osr.CoordinateTransformation(src, dst)
    lon, lat, _ = transform.TransformPoint(500000, 4000000)
    info["projTransformReady"] = math.isfinite(lon) and math.isfinite(lat)
except Exception as e:
    info["projTransformReady"] = False
    info["projTransformError"] = str(e)

# 2. QGIS + Processing (headless)
# 关键：import qgis 先于 initQgis；processing 插件路径需手动加入 sys.path
try:
    import qgis
    from qgis.core import QgsApplication, Qgis
    if hasattr(Qgis, 'version'):
        info["qgisVersion"] = Qgis.version()
    elif hasattr(Qgis, 'QGIS_VERSION'):
        info["qgisVersion"] = Qgis.QGIS_VERSION
    else:
        info["qgisVersion"] = "unknown"
    qgs = QgsApplication([], False)
    qgs.initQgis()
    # 手动将 plugins 加入 sys.path（standalone Python 不会自动加）
    _plugins = os.path.join(os.path.dirname(os.path.dirname(qgis.__file__)), "plugins")
    if _plugins not in sys.path:
        sys.path.insert(0, _plugins)
    from qgis import processing
    from processing.core.Processing import Processing
    Processing.initialize()
    info["processingReady"] = True
    qgs.exitQgis()
except Exception as e:
    info["qgisVersion"] = info.get("qgisVersion", "unknown")
    info["processingReady"] = False
    info["processingError"] = str(e)

print(json.dumps(info))
"""
    try:
        proc = subprocess.run(
            [qgis_python, "-c", probe_script],
            capture_output=True, text=True, timeout=30,
            env=_build_qgis_env(qgis_root),
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
    """探测 QGIS 环境。参照 GeoCode qgis-env.ts 的探测逻辑。

    用 python3.exe + 手动构建 env（PYTHONHOME/PYTHONPATH/PATH），
    优先于 bat-bridge，因为 bat 未必设置正确的 PYTHONPATH。
    """
    qgis_python, qgis_root, bat_path = _find_qgis_python()

    if not qgis_python:
        return {
            "status": "not-installed",
            "reason": (
                "未找到 QGIS 安装。请从 https://qgis.org 下载安装，"
                "安装后 geo_env_status 会自动探测。"
            ),
            "qgis_python": None,
            "qgis_root": None,
        }

    # 优先 bat-bridge（自动处理完整 DLL 环境），回退直接调 python3.exe
    if bat_path:
        result = _probe_qgis_bat(bat_path, qgis_root)
    else:
        result = _probe_qgis_subprocess(qgis_python, qgis_root)

    if "error" in result:
        return {
            "status": "bad",
            "reason": result["error"],
            "qgis_python": qgis_python,
            "qgis_root": qgis_root,
        }

    processing_ready = result.get("processingReady", False) or result.get("processing_ready", False)
    return {
        "status": "ready" if processing_ready else "bad",
        "qgis_python": qgis_python,
        "qgis_root": qgis_root,
        "python_version": result.get("pythonVersion"),
        "gdal_version": result.get("gdalVersion"),
        "qgis_version": result.get("qgisVersion"),
        "processing_ready": processing_ready,
        "proj_transform_ready": result.get("projTransformReady"),
    }


def _probe_qgis_bat(bat_path: str, qgis_root: str = "") -> dict:
    """通过 python-qgis-ltr.bat 探测 QGIS 环境。

    bat 自动设置 DLL PATH，通过临时脚本文件执行。
    qgis_root 用于推导 PYTHONHOME（apps/Python312）。
    """
    import tempfile
    probe_script = b"""import json, math, os, sys
info = {"pythonVersion": ".".join(map(str, sys.version_info[:3]))}
try:
    from osgeo import gdal, osr
    info["gdalVersion"] = gdal.__version__
except Exception as e:
    info["gdalVersion"] = None
    info["gdalError"] = str(e)
    print(json.dumps(info))
    sys.exit(0)
try:
    src = osr.SpatialReference()
    src.ImportFromProj4("+proj=lcc +lat_1=25 +lat_2=47 +lat_0=0 +lon_0=105 +x_0=0 +y_0=0 +datum=WGS84 +units=m +no_defs")
    src.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER)
    dst = osr.SpatialReference()
    dst.ImportFromEPSG(4326)
    dst.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER)
    transform = osr.CoordinateTransformation(src, dst)
    lon, lat, _ = transform.TransformPoint(500000, 4000000)
    info["projTransformReady"] = math.isfinite(lon) and math.isfinite(lat)
except Exception as e:
    info["projTransformReady"] = False
    info["projTransformError"] = str(e)
try:
    import qgis
    from qgis.core import QgsApplication, Qgis
    info["qgisVersion"] = Qgis.QGIS_VERSION if hasattr(Qgis, "QGIS_VERSION") else "unknown"
    qgs = QgsApplication([], False)
    qgs.initQgis()
    _plugins = os.path.join(os.path.dirname(os.path.dirname(qgis.__file__)), "plugins")
    if _plugins not in sys.path:
        sys.path.insert(0, _plugins)
    from qgis import processing
    from processing.core.Processing import Processing
    Processing.initialize()
    info["processingReady"] = True
    qgs.exitQgis()
except Exception as e:
    info["processingReady"] = False
    info["processingError"] = str(e)
print(json.dumps(info))
"""
    tmp_path = None
    try:
        fd, tmp_path = tempfile.mkstemp(suffix=".py", prefix="qgis_probe_")
        os.write(fd, probe_script)
        os.close(fd)

        # 通过 shell=True 调用 bat，设置 PYTHONHOME 确保 stdlib 可定位
        # bat 内部会设置 PATH/PYTHONPATH 等，但需要 PYTHONHOME 预先存在
        env = os.environ.copy()
        python_home = str(Path(qgis_root) / "apps" / "Python312")
        if not Path(python_home).is_dir():
            # 自动探测 Python 版本
            apps_dir = Path(qgis_root) / "apps"
            import re
            for entry in sorted(apps_dir.iterdir(), reverse=True):
                if re.match(r"^Python\d+$", entry.name, re.IGNORECASE):
                    python_home = str(entry)
                    break
        env["PYTHONHOME"] = python_home
        cmd_line = f'"{bat_path}" "{tmp_path}"'
        proc = subprocess.run(
            cmd_line,
            capture_output=True, text=True, timeout=30,
            shell=True, env=env,
        )
        if proc.returncode == 0:
            for line in reversed(proc.stdout.strip().split("\n")):
                line = line.strip()
                if line.startswith("{"):
                    try:
                        return json.loads(line)
                    except json.JSONDecodeError:
                        continue
        return {"error": f"exit code {proc.returncode}", "stderr": proc.stderr[:500]}
    except subprocess.TimeoutExpired:
        return {"error": "QGIS bat 探活超时 (30s)"}
    except Exception as e:
        return {"error": str(e)}
    finally:
        if tmp_path:
            try:
                os.unlink(tmp_path)
            except OSError:
                pass


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
                "reason": (
                    f"GEE 认证已过期或无效。在终端执行:\n"
                    f"  earthengine authenticate\n"
                    f"原始错误: {e}"
                ),
                "ee_version": ee_version,
            }
        if "not signed up" in msg or "project is not registered" in msg:
            return {
                "status": "project-not-registered",
                "reason": (
                    "GCP 项目未注册 Earth Engine。\n"
                    "1. 打开 https://signup.earthengine.google.com\n"
                    "2. 注册你的 GCP 项目以启用 Earth Engine API\n"
                    "3. 等待确认邮件（通常即时）\n"
                    "4. 设置环境变量: set GEE_PROJECT=your-project-id"
                ),
                "ee_version": ee_version,
                "project": project,
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
