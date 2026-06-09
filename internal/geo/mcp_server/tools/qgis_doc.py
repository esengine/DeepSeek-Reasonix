"""qgis_doc — 搜索 QGIS 算法文档 (stub)."""

import logging

logger = logging.getLogger(__name__)


def run(args: dict) -> tuple[str, bool]:
    query = args.get("query", "")
    if not query:
        return "参数错误: 缺少 query", True
    logger.info("qgis_doc stub: query=%s", query)
    return f"[stub] qgis_doc: {query} — QGIS 文档搜索功能待实现 (step 4)", False
