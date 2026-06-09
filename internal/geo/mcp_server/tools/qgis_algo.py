"""run_qgis_algorithm — 调用 QGIS Processing 算法 (stub)."""

import logging

logger = logging.getLogger(__name__)


def run(args: dict) -> tuple[str, bool]:
    algorithm = args.get("algorithm", "")
    params = args.get("params", {})
    if not algorithm:
        return "参数错误: 缺少 algorithm", True
    logger.info("run_qgis_algorithm stub: algo=%s", algorithm)
    return (
        f"[stub] run_qgis_algorithm: {algorithm} params={params} — QGIS 算法调用功能待实现 (step 4)",
        False,
    )
