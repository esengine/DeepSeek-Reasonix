"""qgis_doc — QGIS Processing 算法文档浏览器。

移植自 GeoCode qgis-doc.ts + qgis-registry.ts。
通过 bat-bridge 调用 QGIS Python 导出算法 registry JSON，然后查询。
"""

import json
import logging
from . import qgis_bridge

logger = logging.getLogger(__name__)

# ── Registry 缓存 ─────────────────────────────────────────────────

_registry: dict | None = None
_QGIS_DOCS_BASE = "https://docs.qgis.org/latest/en/docs/user_manual/processing_algs"

DUMP_SCRIPT = """
import json, os, sys, io
from contextlib import redirect_stdout
import qgis
from qgis.core import QgsApplication, QgsProcessingParameterDefinition

ALLOWED = {"native", "gdal", "qgis"}

qgs = QgsApplication([], False)
qgs.initQgis()
# 将 plugins 加入 sys.path（standalone Python 不会自动加）
_plugins = os.path.join(os.path.dirname(os.path.dirname(qgis.__file__)), "plugins")
if _plugins not in sys.path:
    sys.path.insert(0, _plugins)
# processing imports 必须在 initQgis + plugins path 之后
from processing.core.Processing import Processing
import processing
Processing.initialize()

registry = QgsApplication.processingRegistry()
result = {"qgisVersion": "", "providers": []}

try:
    from qgis.core import Qgis
    result["qgisVersion"] = Qgis.version() if hasattr(Qgis, "version") else Qgis.QGIS_VERSION
except Exception:
    pass

for prov in registry.providers():
    if prov.id() not in ALLOWED:
        continue
    p = {"id": prov.id(), "name": prov.name(), "algorithms": []}
    for algo in prov.algorithms():
        buf = io.StringIO()
        with redirect_stdout(buf):
            processing.algorithmHelp(algo.id())
        help_text = buf.getvalue().strip()

        a = {
            "id": algo.id(),
            "name": algo.displayName(),
            "group": algo.group() or "Ungrouped",
            "description": algo.shortHelpString(),
            "helpText": help_text,
            "parameters": [],
            "outputs": [],
        }
        for param in algo.parameterDefinitions():
            dv = param.defaultValue()
            options = []
            try:
                if hasattr(param, "options"):
                    raw = param.options()
                    if raw is not None:
                        options = [str(x) for x in raw]
            except Exception:
                options = []
            a["parameters"].append({
                "name": param.name(),
                "description": param.description(),
                "type": param.type(),
                "defaultValue": str(dv) if dv is not None else None,
                "options": options,
                "optional": bool(param.flags() & QgsProcessingParameterDefinition.Flag.FlagOptional),
            })
        for out in algo.outputDefinitions():
            a["outputs"].append({
                "name": out.name(),
                "description": out.description(),
                "type": out.type(),
            })
        p["algorithms"].append(a)
    result["providers"].append(p)

print(json.dumps(result, ensure_ascii=False))
qgs.exitQgis()
"""


def _ensure_registry() -> dict:
    """获取 QGIS 算法 registry（首次调用时 dump，后续走缓存）。"""
    global _registry
    if _registry is not None:
        return _registry

    proc = qgis_bridge.run_script(DUMP_SCRIPT, timeout=60)
    if proc.returncode != 0:
        raise RuntimeError(f"QGIS registry dump 失败: {proc.stderr[:500]}")
    _registry = json.loads(proc.stdout.strip())
    total = sum(len(p["algorithms"]) for p in _registry["providers"])
    logger.info("registry loaded: %d providers, %d algorithms", len(_registry["providers"]), total)
    return _registry


def _all_algorithms(reg: dict) -> list[dict]:
    return [a for p in reg["providers"] for a in p["algorithms"]]


def _all_groups(reg: dict) -> list[dict]:
    groups: dict[str, int] = {}
    for a in _all_algorithms(reg):
        g = a.get("group", "Ungrouped")
        groups[g] = groups.get(g, 0) + 1
    return sorted(
        [{"name": k, "count": v} for k, v in groups.items()],
        key=lambda x: x["name"].lower(),
    )


def _search(reg: dict, query: str, limit: int = 15) -> list[dict]:
    """简单子串搜索（Python 无 fuzzysort，用大小写不敏感的包含匹配）。"""
    q = query.lower()
    results = []
    for a in _all_algorithms(reg):
        score = 0
        if q == a["id"].lower():
            score = 3
        elif q in a["id"].lower():
            score = 2
        elif q in a["name"].lower():
            score = 1
        elif q in (a.get("description") or "").lower():
            score = 0
        else:
            continue
        results.append((score, a))
    results.sort(key=lambda x: -x[0])
    return [a for _, a in results[:limit]]


def _find_by_id(reg: dict, algo_id: str) -> dict | None:
    # 精确匹配
    for a in _all_algorithms(reg):
        if a["id"] == algo_id:
            return a
    # 去掉前缀匹配（如 "buffer" → "native:buffer"）
    for a in _all_algorithms(reg):
        if a["id"].split(":", 1)[-1] == algo_id.split(":", 1)[-1]:
            return a
    return None


# ── 格式化输出 ────────────────────────────────────────────────────

def _format_algorithm(algo: dict) -> str:
    """格式化单个算法文档。"""
    required = [p for p in algo["parameters"] if not p["optional"]]
    with_defaults = [p for p in algo["parameters"] if p["defaultValue"] is not None]

    lines = [
        f"Algorithm: {algo['id']}",
        f"Name: {algo['name']}",
        f"Group: {algo['group']}",
        "",
    ]

    if algo.get("helpText"):
        if required:
            lines.append(f"Required: {', '.join(p['name'] for p in required)}")
            lines.append("")
        if with_defaults:
            lines.append("Defaults:")
            for p in with_defaults:
                lines.append(f"  {p['name']}: {p['defaultValue']}")
            lines.append("")
        lines.append("Raw QGIS help:")
        lines.append(algo["helpText"])
        lines.append("")
    else:
        if algo.get("description"):
            lines.append(f"Description: {algo['description']}")
            lines.append("")
        for p in algo["parameters"]:
            opt = "optional" if p["optional"] else "required"
            def_val = f", default: {p['defaultValue']}" if p["defaultValue"] else ""
            lines.append(f"- {p['name']}")
            lines.append(f"  Type: {p['type']}, {opt}{def_val}")
            lines.append(f"  {p['description']}")
            if p.get("options"):
                lines.append(f"  Options: {', '.join(p['options'][:10])}")
        lines.append("")

    # external link
    provider = algo["id"].split(":")[0] if ":" in algo["id"] else ""
    slug = algo["name"].lower().replace("&", "").replace(" ", "-")
    slug = "".join(c for c in slug if c.isalnum() or c == "-")
    lines.append(f"External docs: {_QGIS_DOCS_BASE}/{provider}/qgis{algo['id'].replace(':', '')}.html")

    return "\n".join(lines)


# ── 入口 ──────────────────────────────────────────────────────────

def run(args: dict) -> tuple[str, bool]:
    action = args.get("action", "search")
    query = args.get("query", "")
    algorithm = args.get("algorithm", "")
    group = args.get("group", "")

    try:
        reg = _ensure_registry()
    except Exception as e:
        return f"QGIS registry 不可用: {e}\n请确认 QGIS 已安装，运行 geo_env_status 检查。", True

    if action == "list_groups":
        groups = _all_groups(reg)
        lines = [f"{i+1}. {g['name']} ({g['count']})" for i, g in enumerate(groups)]
        return f"QGIS Algorithm Groups ({len(groups)}):\n\n" + "\n".join(lines), False

    elif action == "list_algorithms":
        if not group:
            return '请指定 "group" 参数。先用 action="list_groups" 查看所有分组。', True
        algorithms = [
            a for a in _all_algorithms(reg)
            if a.get("group") == group
        ]
        algorithms.sort(key=lambda a: a["name"].lower())
        if not algorithms:
            return f'分组 "{group}" 中无算法。', True
        lines = [f"{i+1}. {a['id']} — {a['name']}" for i, a in enumerate(algorithms)]
        return f"Algorithms in {group} ({len(algorithms)}):\n\n" + "\n".join(lines), False

    elif action == "read":
        if not algorithm:
            return '请指定 "algorithm" 参数（算法 ID）。', True
        algo = _find_by_id(reg, algorithm)
        if not algo:
            return f'算法 "{algorithm}" 未找到。用 action="search" 搜索。', True
        return _format_algorithm(algo), False

    elif action == "search":
        if not query:
            return '请指定 "query" 参数。', True
        results = _search(reg, query)
        if not results:
            return f'未找到匹配 "{query}" 的算法。', False
        lines = []
        for i, a in enumerate(results):
            desc = a.get("description", "")[:80]
            lines.append(f"{i+1}. {a['id']} ({a['group']}) — {desc}")
        return f"Search: {query} ({len(results)} results)\n\n" + "\n".join(lines), False

    else:
        return f'未知 action: "{action}"。支持: list_groups, list_algorithms, read, search', True
