"""geocode.json 配置加载器。

参照 GeoCode geocode-config.ts 的 schema 和逻辑：
- 自动查找项目根目录下的 geocode.json
- 支持 geocode.jsonc（JSON with comments）
- 首次使用自动生成模板
- 所有工具通过此模块读取用户配置的路径

配置文件格式:
{
    "qgis": {"python": "C:/Program Files/QGIS 3.44.10/bin/python3.exe"},
    "gdal": "D:/Miniconda3/envs/gee/Library/bin",
    "gee": {"python": "D:/Miniconda3/envs/gee/python.exe", "project": "ee-copythree666"}
}
"""

import json
import logging
import os
import re
from pathlib import Path

logger = logging.getLogger(__name__)

_CONFIG: dict | None = None
_CONFIG_DIR: str | None = None

TEMPLATE = """{
    "qgis": {
        "python": "C:/Program Files/QGIS 3.44.10/bin/python3.exe"
    },
    "gdal": "D:/Miniconda3/envs/gee/Library/bin",
    "gee": {
        "python": "D:/Miniconda3/envs/gee/python.exe",
        "project": "ee-copythree666"
    }
}
"""


def _find_project_root() -> str:
    """向上查找 go.mod，确定项目根目录。"""
    current = Path.cwd()
    # 也尝试从当前文件位置查找
    file_dir = Path(__file__).resolve().parent
    for start in (file_dir, current):
        candidate = start
        while candidate != candidate.parent:
            if (candidate / "go.mod").exists():
                return str(candidate)
            candidate = candidate.parent
    # fallback: 当前工作目录
    return str(current)


def _detect_conda_env() -> dict:
    """自动探测 conda 环境的 GDAL 路径。"""
    import sys
    exe = Path(sys.executable)
    conda_env = exe.parent  # D:/.../envs/gee/python.exe → D:/.../envs/gee

    # GDAL bin
    gdal_bin = conda_env / "Library" / "bin"
    gdal_path = str(gdal_bin) if gdal_bin.is_dir() else ""

    # QGIS: 扫描 Program Files
    qgis_python = ""
    if os.name == "nt":
        import re as _re
        for base in [r"C:\Program Files", r"C:\Program Files (x86)"]:
            base_p = Path(base)
            if not base_p.is_dir():
                continue
            try:
                for entry in base_p.iterdir():
                    if not entry.is_dir() or "qgis" not in entry.name.lower():
                        continue
                    py = entry / "bin" / "python3.exe"
                    if py.exists():
                        qgis_python = str(py)
                        break
                if qgis_python:
                    break
            except PermissionError:
                continue

    return {
        "gdal": gdal_path,
        "qgis_python": qgis_python,
        "gee_python": str(exe),
    }


def load() -> dict:
    """加载 geocode.json 配置（首次调用时读取，后续走缓存）。"""
    global _CONFIG, _CONFIG_DIR

    if _CONFIG is not None:
        return _CONFIG

    root = _find_project_root()
    logger.info("project root: %s", root)

    # 查找 geocode.json / geocode.jsonc
    candidates = [
        os.path.join(root, "geocode.json"),
        os.path.join(root, "geocode.jsonc"),
        os.path.join(root, ".reasonix", "geocode.json"),
    ]

    config = {}
    loaded = False

    for path in candidates:
        if os.path.exists(path):
            try:
                with open(path, "r", encoding="utf-8") as f:
                    text = f.read()
                # 去除 BOM
                text = text.lstrip("﻿")
                # 简易 JSONC 支持：去掉 // 和 /* */ 注释
                text = _strip_json_comments(text)
                config = json.loads(text)
                logger.info("loaded config from %s", path)
                loaded = True
                break
            except Exception as e:
                logger.warning("failed to parse %s: %s", path, e)

    if not loaded:
        # 自动生成模板
        template_path = os.path.join(root, "geocode.json")
        auto = _detect_conda_env()
        template = {
            "gdal": auto["gdal"],
            "gee": {
                "python": auto["gee_python"],
                "project": os.environ.get("GEE_PROJECT", ""),
            },
        }
        if auto.get("qgis_python"):
            template["qgis"] = {"python": auto["qgis_python"]}
        try:
            with open(template_path, "w", encoding="utf-8") as f:
                json.dump(template, f, indent=2, ensure_ascii=False)
            logger.info("auto-generated geocode.json at %s", template_path)
            config = template
        except Exception as e:
            logger.warning("cannot write geocode.json: %s", e)

    _CONFIG = config
    _CONFIG_DIR = root
    return _CONFIG


def _strip_json_comments(text: str) -> str:
    """去掉 JSON 中的 // 和 /* */ 注释。"""
    # 去掉多行注释
    text = re.sub(r"/\*.*?\*/", "", text, flags=re.DOTALL)
    # 去掉单行注释（不删除字符串内的 //）
    lines = []
    for line in text.split("\n"):
        # naive: 简单场景下够用
        if "//" in line:
            line = line[: line.index("//")]
        lines.append(line)
    return "\n".join(lines)


def get_qgis_python() -> str | None:
    """获取用户配置的 QGIS Python 路径。"""
    cfg = load()
    qgis = cfg.get("qgis", {})
    if isinstance(qgis, dict):
        return qgis.get("python") or None
    return None


def get_gdal_bin() -> str | None:
    """获取用户配置的 GDAL bin 目录。"""
    cfg = load()
    gdal = cfg.get("gdal", "")
    if isinstance(gdal, str) and gdal:
        return gdal
    return None


def get_gee_python() -> str | None:
    """获取用户配置的 GEE Python 路径。"""
    cfg = load()
    gee = cfg.get("gee", {})
    if isinstance(gee, dict):
        return gee.get("python") or None
    return None


def get_gee_project() -> str | None:
    """获取用户配置的 GEE project ID。"""
    cfg = load()
    gee = cfg.get("gee", {})
    if isinstance(gee, dict):
        return gee.get("project") or None
    return None


def get_config_dir() -> str:
    """获取配置目录（项目根目录）。"""
    load()  # ensure initialized
    return _CONFIG_DIR or _find_project_root()


def reload():
    """强制重新加载配置。"""
    global _CONFIG
    _CONFIG = None
    return load()
