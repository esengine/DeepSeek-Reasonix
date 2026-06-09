"""run_gee_script — 执行 GEE Python 脚本 (stub)."""

import logging

logger = logging.getLogger(__name__)


def run(args: dict) -> tuple[str, bool]:
    script = args.get("script", "")
    if not script:
        return "参数错误: 缺少 script", True
    logger.info("run_gee_script stub: script length=%d", len(script))
    return (
        f"[stub] run_gee_script: 脚本已提交（{len(script)} 字符）— GEE 执行功能待实现 (step 5)",
        False,
    )
