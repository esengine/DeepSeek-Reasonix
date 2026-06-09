"""Tool registry — 5 GeoCode MCP tools, schemas, and dispatch."""

import logging

from .tools.read_geo import run as read_geo_run
from .tools.qgis_algo import run as qgis_algo_run
from .tools.qgis_doc import run as qgis_doc_run
from .tools.gee import run as gee_run
from .tools.env_status import run as env_status_run

logger = logging.getLogger(__name__)

# ── tool definitions ──────────────────────────────────────────────

TOOLS = [
    {
        "name": "read_geo_data",
        "description": (
            "读取矢量/栅格遥感数据的元数据和预览。"
            "支持 14 种格式（GeoTIFF, Shapefile, GeoJSON, GeoPackage 等）。"
            "返回投影、范围、波段数/字段列表等元信息，以及地图预览 URL。"
        ),
        "inputSchema": {
            "type": "object",
            "properties": {
                "path": {
                    "type": "string",
                    "description": "遥感数据文件的绝对路径",
                }
            },
            "required": ["path"],
        },
        "annotations": {
            "readOnlyHint": True,
            "title": "读取遥感数据",
        },
    },
    {
        "name": "run_qgis_algorithm",
        "description": (
            "调用 QGIS Processing 工具箱中的算法。"
            "支持 native、gdal、qgis、grass、saga 等 Provider 的全部算法。"
            "参数通过 params 字典传入，键为算法参数名，值为参数值。"
        ),
        "inputSchema": {
            "type": "object",
            "properties": {
                "algorithm": {
                    "type": "string",
                    "description": "QGIS 算法 ID，如 'native:buffer'、'gdal:contour'",
                },
                "params": {
                    "type": "object",
                    "description": "算法参数字典，键值对应 QGIS processing.run() 的 params 参数",
                },
            },
            "required": ["algorithm", "params"],
        },
        "annotations": {
            "readOnlyHint": False,
            "title": "运行 QGIS 算法",
        },
    },
    {
        "name": "run_gee_script",
        "description": (
            "执行 Google Earth Engine Python 脚本。"
            "脚本可引用预注入的 geocode 辅助模块（init_gee、load_region、download_image、"
            "check_coverage、heartbeat 等）。"
            "支持 idle timeout 检测（60s 无输出则终止），长时间操作用 heartbeat() 保活。"
        ),
        "inputSchema": {
            "type": "object",
            "properties": {
                "script": {
                    "type": "string",
                    "description": "内联 GEE Python 脚本代码",
                },
                "script_path": {
                    "type": "string",
                    "description": "GEE Python 脚本文件绝对路径（与 script 二选一）",
                },
            },
        },
        "annotations": {
            "readOnlyHint": False,
            "title": "运行 GEE 脚本",
        },
    },
    {
        "name": "qgis_doc",
        "description": (
            "浏览和搜索 QGIS Processing 工具箱算法文档。"
            "支持四种操作：list_groups（列出分组）、list_algorithms（列出分组内算法）、"
            "read（查看算法详情含参数 schema）、search（关键词搜索）。"
        ),
        "inputSchema": {
            "type": "object",
            "properties": {
                "action": {
                    "type": "string",
                    "enum": ["list_groups", "list_algorithms", "read", "search"],
                    "description": "操作类型: list_groups/list_algorithms/read/search",
                },
                "group": {
                    "type": "string",
                    "description": "分组名（list_algorithms 时必填）",
                },
                "algorithm": {
                    "type": "string",
                    "description": "算法 ID，如 'native:buffer'（read 时必填）",
                },
                "query": {
                    "type": "string",
                    "description": "搜索关键词（search 时必填）",
                },
            },
            "required": ["action"],
        },
        "annotations": {
            "readOnlyHint": True,
            "title": "QGIS 算法文档",
        },
    },
    {
        "name": "geo_env_status",
        "description": (
            "探测 GDAL、QGIS、GEE 三重遥感环境的可用状态。"
            "返回各环境的分级诊断结果和 AI 配置指引。"
            "GDAL: ready / raster-only / vector-only / bad。"
            "QGIS: ready / probing / bad。"
            "GEE:  ready / auth-required / project-not-registered / not-installed / init-failed。"
        ),
        "inputSchema": {
            "type": "object",
            "properties": {},
        },
        "annotations": {
            "readOnlyHint": True,
            "title": "遥感环境状态",
        },
    },
]

# ── handler dispatch table ────────────────────────────────────────

_HANDLERS = {
    "read_geo_data": read_geo_run,
    "run_qgis_algorithm": qgis_algo_run,
    "run_gee_script": gee_run,
    "qgis_doc": qgis_doc_run,
    "geo_env_status": env_status_run,
}


def tool_list() -> list[dict]:
    """Return the MCP tools/list payload."""
    return TOOLS


def call_tool(name: str, arguments: dict) -> tuple[str, bool]:
    """Dispatch to the matching tool handler. Returns (text, is_error)."""
    handler = _HANDLERS.get(name)
    if handler is None:
        return f"unknown tool: {name}", True
    return handler(arguments)
