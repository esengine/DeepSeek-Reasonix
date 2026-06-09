"""read_geo_data — 读取矢量/栅格遥感数据的元数据和预览。

栅格：gdalinfo 元数据 + 降采样 WebP 预览
矢量：ogrinfo 元数据（字段统计、几何类型）+ GeoJSON 转换

预览通过 http_server 的随机端口 HTTP URL 返回，不经过 LLM token。
"""

import json
import logging
import math
import os
import re
from pathlib import Path

from osgeo import gdal, ogr, osr

from .. import http_server

gdal.UseExceptions()
ogr.UseExceptions()

logger = logging.getLogger(__name__)

# ── 格式支持 ──────────────────────────────────────────────────────

VECTOR_EXTS = {".shp", ".geojson", ".json", ".gpkg", ".gml", ".kml", ".gpx", ".fgb"}
RASTER_EXTS = {".tif", ".tiff", ".img", ".asc", ".dem", ".bil", ".hdr", ".nc", ".hdf"}

# ── CRS 工具（移植自 GeoCode raster_read.py / vector_read.py）─────

WGS84 = osr.SpatialReference()
try:
    WGS84.ImportFromEPSG(4326)
    WGS84.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER)
except Exception:
    WGS84 = None

_PROJ_FRIENDLY_NAMES = {
    "lcc": "Lambert Conformal Conic", "tmerc": "Transverse Mercator",
    "utm": "UTM", "merc": "Mercator", "webmerc": "Web Mercator",
    "aea": "Albers Equal Area", "laea": "Lambert Azimuthal Equal Area",
    "aeqd": "Azimuthal Equidistant", "stere": "Stereographic",
    "omerc": "Oblique Mercator", "longlat": "Geographic",
    "eqc": "Equidistant Cylindrical", "sinu": "Sinusoidal",
    "moll": "Mollweide", "robin": "Robinson", "krovak": "Krovak",
}
_PROJ4_PROJ_RE = re.compile(r'(?:^|\s)\+proj=(?:"([^"]+)"|\'([^\']+)\'|([^\s]+))')


def _friendly_name(srs):
    """解析人类可读的 CRS 名称。"""
    name = srs.GetAttrValue("PROJCS") or srs.GetAttrValue("GEOGCS")
    if name and name.strip().lower() not in ("unknown", "unnamed", "unnamed crs"):
        return name

    projection = srs.GetAttrValue("PROJECTION")
    if projection and projection.strip().lower() not in ("unknown", "unnamed"):
        return projection.strip().replace("_", " ") + " (custom)"

    try:
        proj4 = srs.ExportToProj4() or ""
    except Exception:
        proj4 = ""
    match = _PROJ4_PROJ_RE.search(proj4)
    if match:
        proj = (match.group(1) or match.group(2) or match.group(3)).lower()
        if proj in _PROJ_FRIENDLY_NAMES:
            return _PROJ_FRIENDLY_NAMES[proj] + " (custom)"
    return "Custom projection"


def _resolve_crs(srs):
    """返回 CRS 标识符（EPSG:xxxx 或 Proj4 字符串）+ 名称。"""
    if not srs:
        return None, None

    name = _friendly_name(srs)
    try:
        srs.AutoIdentifyEPSG()
    except Exception:
        pass

    code = srs.GetAuthorityCode(None)
    authority = srs.GetAuthorityName(None)
    if not code:
        code = srs.GetAuthorityCode("PROJCS")
        authority = srs.GetAuthorityName("PROJCS")

    if code and authority:
        return f"{authority}:{code}", name

    try:
        proj4 = srs.ExportToProj4().strip()
    except Exception:
        proj4 = ""
    if proj4:
        return proj4, name

    return srs.ExportToWkt(), name


def _extent_to_wgs84(srs, xmin, xmax, ymin, ymax):
    """将范围转换到 WGS84。"""
    if not srs or not WGS84:
        return None
    src = srs.Clone()
    src.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER)
    if src.IsSame(WGS84):
        return {"xmin": xmin, "xmax": xmax, "ymin": ymin, "ymax": ymax}
    try:
        transform = osr.CoordinateTransformation(src, WGS84)
        corners = [(xmin, ymin), (xmin, ymax), (xmax, ymin), (xmax, ymax)]
        lons, lats = [], []
        for x, y in corners:
            lon, lat, _ = transform.TransformPoint(x, y)
            lons.append(lon)
            lats.append(lat)
        return {"xmin": min(lons), "xmax": max(lons), "ymin": min(lats), "ymax": max(lats)}
    except Exception:
        return None


def _safe_float(v):
    """inf/nan → JSON-safe string。"""
    if v is None:
        return None
    if math.isinf(v):
        return "Infinity" if v > 0 else "-Infinity"
    if math.isnan(v):
        return "NaN"
    return v


# ── 栅格读取 ──────────────────────────────────────────────────────

# 扩展名 → 驱动名映射
_EXT_DRIVER = {
    ".img": "HFA", ".asc": "AAIGrid", ".dem": "AAIGrid",
    ".bil": "EHdr", ".hdr": "EHdr", ".nc": "netCDF", ".hdf": "HDF4",
}

MAX_PREVIEW_SIDE = 4096


def _read_raster_meta(path: str) -> dict:
    """读取栅格元数据。移植自 GeoCode raster_read.py。"""
    ds = gdal.Open(path)
    if ds is None:
        raise RuntimeError(f"无法打开栅格文件: {path}")

    gt = ds.GetGeoTransform()
    no_geotransform = gt == (0.0, 1.0, 0.0, 0.0, 0.0, 1.0)

    wkt = ds.GetProjection()
    srs, _crs_warn = None, None
    if wkt and wkt.strip():
        srs = osr.SpatialReference()
        try:
            srs.ImportFromWkt(wkt)
            srs.SetAxisMappingStrategy(osr.OAMS_TRADITIONAL_GIS_ORDER)
        except Exception:
            srs = None

    xmin = gt[0]
    ymax = gt[3]
    xmax = xmin + gt[1] * ds.RasterXSize
    ymin = ymax + gt[5] * ds.RasterYSize

    crs, crs_name = _resolve_crs(srs)

    extent_wgs84 = None if no_geotransform else _extent_to_wgs84(
        srs, xmin, xmax, ymin, ymax
    )

    result = {
        "data_type": "raster",
        "driver": ds.GetDriver().ShortName,
        "path": path,
        "size": {"width": ds.RasterXSize, "height": ds.RasterYSize},
        "band_count": ds.RasterCount,
        "crs": crs,
        "crs_name": crs_name,
        "pixel_size": {"x": gt[1], "y": abs(gt[5])},
        "extent": {"xmin": xmin, "xmax": xmax, "ymin": ymin, "ymax": ymax},
        "extent_wgs84": extent_wgs84,
        "bands": [],
    }

    if no_geotransform:
        result["warning"] = (
            "影像无地理参考（GeoTransform 为单位矩阵）。"
            "范围值为像素坐标而非地理坐标，不可用于投影变换。"
        )

    for i in range(1, ds.RasterCount + 1):
        band = ds.GetRasterBand(i)
        nodata = band.GetNoDataValue()
        stats = band.GetStatistics(True, True)
        result["bands"].append({
            "index": i,
            "data_type": gdal.GetDataTypeName(band.DataType),
            "nodata": _safe_float(nodata),
            "statistics": {
                "min": _safe_float(stats[0]),
                "max": _safe_float(stats[1]),
                "mean": _safe_float(stats[2]),
                "stddev": _safe_float(stats[3]),
            },
            "color_interp": gdal.GetColorInterpretationName(band.GetColorInterpretation()),
        })

    ds = None
    return result


def _select_bands(ds):
    """选择 RGB 波段组合。移植自 GeoCode raster_preview.py。"""
    band_count = ds.RasterCount

    bands = []
    for i in range(1, band_count + 1):
        b = ds.GetRasterBand(i)
        bands.append({
            "idx": i,
            "color": gdal.GetColorInterpretationName(b.GetColorInterpretation()).lower(),
            "desc": (b.GetDescription() or "").upper().strip(),
        })

    alpha = next((b["idx"] for b in bands if b["color"] == "alpha"), None)

    def pick(n):
        for b in bands:
            if not b["desc"]:
                continue
            if b["desc"] in (f"B{n}", f"B0{n}", f"BAND {n}"):
                return b["idx"]
        return None

    # 单波段
    single = (band_count == 1) or (band_count <= 2 and bands[0]["color"] == "gray")
    if single:
        return (1, 1, 1), (alpha if alpha else (2 if band_count >= 2 else None))

    # 显式 RGB 颜色解释
    red = next((b["idx"] for b in bands if b["color"] == "red"), None)
    green = next((b["idx"] for b in bands if b["color"] == "green"), None)
    blue = next((b["idx"] for b in bands if b["color"] == "blue"), None)
    if red and green and blue:
        return (red, green, blue), alpha

    # 描述匹配 B4/B3/B2
    b4, b3, b2 = pick(4), pick(3), pick(2)
    if b4 and b3 and b2:
        return (b4, b3, b2), alpha

    if band_count >= 4:
        return (4, 3, 2), alpha
    if band_count >= 3:
        return (1, 2, 3), alpha
    if band_count == 2:
        return (1, 1, 1), 2
    return (1, 1, 1), None


def _preview_size(width, height):
    """计算预览尺寸，最长边不超过 MAX_PREVIEW_SIDE，不放大。"""
    side = max(width, height)
    if side <= MAX_PREVIEW_SIDE:
        return width, height
    ratio = MAX_PREVIEW_SIDE / side
    return max(1, round(width * ratio)), max(1, round(height * ratio))


def _compute_stretch(band):
    """mean ± 1.8*stddev 线性拉伸。"""
    stats = band.GetStatistics(True, True)
    bmin, bmax, mean, stddev = stats[0], stats[1], stats[2], stats[3]
    if not math.isfinite(bmin) or not math.isfinite(bmax) or bmax <= bmin:
        return 0.0, 255.0
    if not math.isfinite(mean) or not math.isfinite(stddev) or stddev <= 0:
        return bmin, bmax
    low = max(bmin, mean - stddev * 1.8)
    high = min(bmax, mean + stddev * 1.8)
    if high <= low:
        return bmin, bmax
    return low, high


def _generate_raster_preview(path: str) -> dict:
    """生成栅格 WebP 预览，存入 HTTP Server 缓存。"""
    ds = gdal.Open(path)
    if ds is None:
        raise RuntimeError(f"无法打开栅格文件: {path}")

    width, height = ds.RasterXSize, ds.RasterYSize
    out_w, out_h = _preview_size(width, height)
    rgb_indices, alpha_idx = _select_bands(ds)

    # 读取并拉伸 RGB 波段
    import numpy as np
    channels = []
    for band_idx in rgb_indices:
        band = ds.GetRasterBand(band_idx)
        nodata = band.GetNoDataValue()
        low, high = _compute_stretch(band)

        data = band.ReadAsArray(buf_xsize=out_w, buf_ysize=out_h).astype(np.float64)

        # nodata mask
        if nodata is not None:
            if math.isfinite(nodata):
                mask = data == nodata
            elif math.isnan(nodata):
                mask = np.isnan(data)
            else:
                mask = np.zeros_like(data, dtype=bool)
        else:
            mask = np.isnan(data)

        if high > low:
            stretched = (data - low) / (high - low) * 255.0
        else:
            stretched = np.full_like(data, 128.0)

        stretched = np.clip(stretched, 0, 255).astype(np.uint8)
        channels.append((stretched, mask))

    # Alpha 通道
    combined_mask = channels[0][1]
    for _, m in channels[1:]:
        combined_mask = combined_mask | m

    if alpha_idx:
        alpha_band = ds.GetRasterBand(alpha_idx)
        alpha_data = alpha_band.ReadAsArray(buf_xsize=out_w, buf_ysize=out_h).astype(np.float64)
        amin, amax = float(alpha_data.min()), float(alpha_data.max())
        if amax > amin:
            alpha_arr = ((alpha_data - amin) / (amax - amin) * 255.0).astype(np.uint8)
        else:
            alpha_arr = np.full((out_h, out_w), 255, dtype=np.uint8)
    else:
        alpha_arr = np.full((out_h, out_w), 255, dtype=np.uint8)

    alpha_arr[combined_mask] = 0
    ds = None

    # 合成 RGBA → WebP via /vsimem/
    mem_drv = gdal.GetDriverByName("MEM")
    out_ds = mem_drv.Create("", out_w, out_h, 4, gdal.GDT_Byte)
    for i, (ch, _) in enumerate(channels):
        out_ds.GetRasterBand(i + 1).WriteArray(ch)
    out_ds.GetRasterBand(4).WriteArray(alpha_arr)

    webp_path = "/vsimem/preview.webp"
    webp_drv = gdal.GetDriverByName("WEBP")
    webp_drv.CreateCopy(webp_path, out_ds, options=["QUALITY=75"])
    out_ds = None

    # 读取 /vsimem/ 中的 WebP 字节
    vsi = gdal.VSIFOpenL(webp_path, "rb")
    gdal.VSIFSeekL(vsi, 0, 2)
    size = gdal.VSIFTellL(vsi)
    gdal.VSIFSeekL(vsi, 0, 0)
    webp_bytes = gdal.VSIFReadL(1, size, vsi)
    gdal.VSIFCloseL(vsi)
    gdal.Unlink(webp_path)

    # 计算原始范围（用于前端叠加）
    gt = gdal.Open(path).GetGeoTransform()
    extent = {
        "xmin": gt[0],
        "ymin": gt[3] + gt[5] * gdal.Open(path).RasterYSize,
        "xmax": gt[0] + gt[1] * gdal.Open(path).RasterXSize,
        "ymax": gt[3],
    }

    return {
        "preview_width": out_w,
        "preview_height": out_h,
        "original_width": width,
        "original_height": height,
        "rgb_bands": list(rgb_indices),
        "extent": extent,
        "webp_bytes": webp_bytes,
        "webp_size": len(webp_bytes),
    }


# ── 矢量读取 ──────────────────────────────────────────────────────

def _read_vector_meta(path: str) -> dict:
    """读取矢量元数据。移植自 GeoCode vector_read.py。"""
    ds = ogr.Open(path)
    if ds is None:
        raise RuntimeError(f"无法打开矢量文件: {path}")

    driver = ds.GetDriver().GetName()
    layer_count = ds.GetLayerCount()

    if layer_count == 0:
        raise RuntimeError(f"矢量文件无图层: {path}")

    layer = ds.GetLayerByIndex(0)
    srs = layer.GetSpatialRef()
    defn = layer.GetLayerDefn()

    # 字段信息
    fields = []
    for j in range(defn.GetFieldCount()):
        fd = defn.GetFieldDefn(j)
        fields.append({"name": fd.GetName(), "type": fd.GetTypeName()})

    # 范围
    extent = layer.GetExtent()
    crs, crs_name = _resolve_crs(srs)

    # 采样特征（最多 5 个）
    features = []
    layer.ResetReading()
    for _ in range(5):
        feat = layer.GetNextFeature()
        if feat is None:
            break
        geom = feat.GetGeometryRef()
        attrs = {}
        for j in range(defn.GetFieldCount()):
            attrs[defn.GetFieldDefn(j).GetName()] = feat.GetField(j)
        features.append({
            "fid": feat.GetFID(),
            "attributes": attrs,
            "geometry_type": geom.GetGeometryName() if geom else None,
        })

    result = {
        "data_type": "vector",
        "driver": driver,
        "path": path,
        "layer_count": layer_count,
        "layer_name": layer.GetName(),
        "geometry_type": ogr.GeometryTypeToName(layer.GetGeomType()),
        "feature_count": layer.GetFeatureCount(),
        "field_count": len(fields),
        "fields": fields,
        "crs": crs,
        "crs_name": crs_name,
        "extent": {
            "xmin": extent[0], "xmax": extent[1],
            "ymin": extent[2], "ymax": extent[3],
        },
        "extent_wgs84": _extent_to_wgs84(
            srs, extent[0], extent[1], extent[2], extent[3]
        ),
        "sample_features": features,
    }

    # GPKG 多图层概览
    if driver == "GPKG" and layer_count > 1:
        layers_summary = []
        for i in range(layer_count):
            lyr = ds.GetLayerByIndex(i)
            layers_summary.append({
                "index": i,
                "name": lyr.GetName(),
                "geometry_type": ogr.GeometryTypeToName(lyr.GetGeomType()),
                "feature_count": lyr.GetFeatureCount(),
            })
        result["layers"] = layers_summary

    ds = None
    return result


def _generate_vector_geojson(path: str, max_features: int = 5000) -> dict:
    """将矢量文件转为 GeoJSON（EPSG:3857），存入 HTTP Server。"""
    # 用 ogr2ogr 转换
    import subprocess
    import tempfile

    tmpdir = tempfile.mkdtemp(prefix="geocode_")
    out_path = os.path.join(tmpdir, "output.geojson")

    try:
        cmd = [
            "ogr2ogr", "-f", "GeoJSON",
            "-t_srs", "EPSG:3857",
            "-limit", str(max_features),
            out_path, path,
        ]
        proc = subprocess.run(cmd, capture_output=True, text=True, timeout=120)
        if proc.returncode != 0:
            raise RuntimeError(f"ogr2ogr 转换失败: {proc.stderr[:500]}")

        with open(out_path, "r", encoding="utf-8") as f:
            geojson = json.load(f)

        return geojson
    finally:
        # 清理临时文件
        try:
            os.remove(out_path)
        except OSError:
            pass
        try:
            os.rmdir(tmpdir)
        except OSError:
            pass


# ── 入口 ──────────────────────────────────────────────────────────

def _detect_type(path: str) -> str:
    """根据扩展名判断数据类型。"""
    ext = Path(path).suffix.lower()
    if ext in VECTOR_EXTS:
        return "vector"
    if ext in RASTER_EXTS:
        return "raster"
    # GDAL 探测
    ds = ogr.Open(path)
    if ds is not None:
        ds = None
        return "vector"
    ds = gdal.Open(path)
    if ds is not None:
        ds = None
        return "raster"
    raise RuntimeError(f"无法识别数据格式: {path}")


def run(args: dict) -> tuple[str, bool]:
    path = args.get("path", "")
    if not path:
        return "参数错误: 缺少 path", True

    path = os.path.abspath(os.path.expanduser(path))
    if not os.path.exists(path):
        return f"文件不存在: {path}", True

    # 确保 HTTP Server 已启动
    http_port = http_server.start()
    http_base = f"http://127.0.0.1:{http_port}"

    try:
        data_type = _detect_type(path)
    except Exception as e:
        return f"无法识别数据格式: {e}", True

    try:
        if data_type == "raster":
            meta = _read_raster_meta(path)

            # 生成预览
            try:
                preview = _generate_raster_preview(path)
                preview_path = http_server.store_webp(preview["webp_bytes"])
            except Exception as e:
                logger.warning("raster preview failed: %s", e)
                preview = None
                preview_path = None

            result = {
                "__geo_type__": "raster_preview",
                "metadata": meta,
                "preview_url": f"{http_base}{preview_path}" if preview_path else None,
                "preview_width": preview["preview_width"] if preview else None,
                "preview_height": preview["preview_height"] if preview else None,
                "preview_size_bytes": preview["webp_size"] if preview else None,
                "rgb_bands": preview["rgb_bands"] if preview else None,
                "http_port": http_port,
            }

        else:
            meta = _read_vector_meta(path)

            # 生成 GeoJSON
            try:
                geojson = _generate_vector_geojson(path, max_features=5000)
                geojson_path = http_server.store_geojson(geojson)
            except Exception as e:
                logger.warning("vector geojson generation failed: %s", e)
                geojson = None
                geojson_path = None

            result = {
                "__geo_type__": "vector_preview",
                "metadata": meta,
                "geojson_url": f"{http_base}{geojson_path}" if geojson_path else None,
                "feature_count": meta["feature_count"],
                "http_port": http_port,
            }

        # 输出格式化文本（给 LLM 看） + JSON marker（给前端解析）
        text_lines = [
            json.dumps(result, ensure_ascii=False, default=str),
        ]
        return "\n".join(text_lines), False

    except Exception as e:
        logger.exception("read_geo_data failed: %s", path)
        return f"读取失败: {e}", True
