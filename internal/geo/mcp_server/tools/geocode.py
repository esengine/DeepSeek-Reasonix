"""GEE 辅助模块 — 移植自 GeoCode geocode.py。

init_gee(), load_region(), download_image(), heartbeat(), check_coverage()
作为内置 helper 注入 GEE 脚本的 PYTHONPATH。
"""

import os
import threading
import time


class _Heartbeat:
    """后台心跳线程，定期打印状态防止 idle timeout。"""

    def __init__(self, message, interval=30):
        self._message = message
        self._interval = interval
        self._start = time.time()
        self._stop = threading.Event()
        self._thread = threading.Thread(target=self._run, daemon=True)
        self._thread.start()

    def _run(self):
        while not self._stop.wait(self._interval):
            elapsed = int(time.time() - self._start)
            print(f"[{elapsed}s] {self._message}", flush=True)

    def stop(self):
        self._stop.set()
        self._thread.join(timeout=1)

    def elapsed(self):
        return int(time.time() - self._start)

    def __enter__(self):
        return self

    def __exit__(self, *args):
        self.stop()


def heartbeat(message, interval=30):
    """后台心跳，用于长时间运行的 GEE 操作保持进程活跃。

        with heartbeat("Computing NDVI statistics"):
            stats = image.reduceRegion(...).getInfo()
    """
    print(f"{message}...", flush=True)
    return _Heartbeat(message, interval)


def init_gee(project_id=None):
    """初始化 GEE。

    从 GEE_PROJECT 环境变量读取 project ID。
    """
    import ee

    if project_id is None:
        project_id = os.environ.get("GEE_PROJECT")
    if not project_id:
        raise RuntimeError(
            "GEE project ID 未设置。请在 geocode.json 中配置 gee.project，"
            "或传递 project_id=... 参数。"
        )

    print(f"Initializing GEE (project: {project_id})...", flush=True)
    try:
        ee.Initialize(project=project_id)
    except Exception as e:
        raise RuntimeError(
            f"GEE Initialize 失败: {e}\n"
            f"  Project: {project_id}\n"
            "如果凭证过期，在终端执行: earthengine authenticate\n"
            "然后重试。"
        ) from e

    print(f"GEE ready (earthengine-api {ee.__version__})", flush=True)


def load_region(file_path):
    """从本地矢量文件加载单个要素为 ee.FeatureCollection。

    自动重投影到 EPSG:4326。
    要求文件包含恰好一个要素。
    """
    import ee
    import json

    try:
        import geopandas as gpd
    except ImportError:
        raise RuntimeError(
            "load_region() 需要 geopandas。安装: pip install geopandas"
        )

    if not os.path.exists(file_path):
        raise FileNotFoundError(f"区域文件不存在: {file_path}")

    name = os.path.basename(file_path)
    print(f"Loading region: {name}", flush=True)
    hb = _Heartbeat(f"Loading {name}")

    try:
        gdf = gpd.read_file(file_path)

        if gdf.crs is None:
            raise ValueError(
                f"矢量文件无 CRS 元数据: {file_path}\n"
                "Shapefile 需要 .prj 文件；GeoPackage/GeoJSON 需要内嵌 CRS。"
            )

        if len(gdf) != 1:
            raise ValueError(
                f"load_region 需要恰好 1 个要素，当前文件有 {len(gdf)} 个。"
            )

        src_crs = gdf.crs
        try:
            src_epsg = src_crs.to_epsg()
        except Exception:
            src_epsg = None

        if src_epsg != 4326:
            print(f"Reprojecting from {src_crs} to EPSG:4326", flush=True)
            gdf = gdf.to_crs(4326)

        geometry = gdf.geometry.iloc[0]
        if geometry is None or geometry.is_empty:
            raise ValueError("区域几何为空。")

        gdf = gdf[[gdf.geometry.name]]
        geojson = json.loads(gdf.to_json())
        fc = ee.FeatureCollection(geojson["features"])
    finally:
        hb.stop()

    print(f"Region loaded: 1 feature ({hb.elapsed()}s)", flush=True)
    return fc


def check_coverage(image, roi, band=None, scale=30):
    """检查影像在 ROI 内的有效像素覆盖率。返回 [0.0, 1.0]。"""
    import ee

    check_band = image.select([band]) if band else image.select([0])
    valid_img = check_band.mask().unmask(0).gt(0).rename("valid")
    all_img = ee.Image.constant(1).rename("all").reproject(valid_img.projection())

    print("Checking coverage...", flush=True)
    hb = _Heartbeat("Checking coverage")
    try:
        stats = (
            valid_img.addBands(all_img)
            .reduceRegion(
                reducer=ee.Reducer.sum(),
                geometry=roi,
                scale=scale,
                maxPixels=1e13,
            )
            .getInfo()
        )
    finally:
        hb.stop()

    valid_pixels = int(stats.get("valid") or 0)
    total_pixels = int(stats.get("all") or 0)
    coverage = valid_pixels / total_pixels if total_pixels > 0 else 0
    print(f"Coverage: {coverage:.1%} | valid: {valid_pixels}/{total_pixels}", flush=True)
    return round(coverage, 4)


def download_image(image, filename, region, scale=10, crs="EPSG:4326", dtype=None):
    """下载 ee.Image 到本地 GeoTIFF。使用 geemap 的分块下载。"""
    import sys
    import shutil
    import tempfile

    try:
        import geemap
    except ImportError:
        raise RuntimeError("download_image() 需要 geemap。安装: pip install geemap geedim")

    try:
        import geedim  # noqa
    except ImportError:
        raise RuntimeError("download_image() 需要 geedim。安装: pip install geedim")

    filename = os.path.abspath(os.path.expanduser(filename))
    parent = os.path.dirname(filename)
    if not os.path.isdir(parent):
        raise FileNotFoundError(f"目录不存在: {parent}")

    name = os.path.basename(filename)
    tmp_fd, tmp_path = tempfile.mkstemp(suffix=".tif")
    os.close(tmp_fd)

    print(f"Downloading: {name} (scale={scale}, crs={crs})", flush=True)
    hb = _Heartbeat(f"Downloading {name}")
    try:
        geemap.download_ee_image(
            image,
            filename=tmp_path,
            region=region,
            scale=scale,
            crs=crs,
            dtype=dtype,
        )
    except BaseException:
        try:
            os.remove(tmp_path)
        except OSError:
            pass
        raise
    finally:
        hb.stop()

    try:
        shutil.move(tmp_path, filename)
    except Exception:
        try:
            os.remove(tmp_path)
        except OSError:
            pass
        raise

    size_mb = os.path.getsize(filename) / (1024 * 1024)
    print(f"Download completed: {filename} ({size_mb:.1f} MB, {hb.elapsed()}s)", flush=True)
    return filename
