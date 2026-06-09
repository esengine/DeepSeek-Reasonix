"""run_qgis_algorithm — 调用 QGIS Processing 工具箱算法。

移植自 GeoCode run-processing.ts。
通过 bat-bridge 在 QGIS Python 环境中执行算法。
"""

import json
import logging
import time
from . import qgis_bridge

logger = logging.getLogger(__name__)

# QGIS Processing 执行脚本（移植自 RUN_PROCESSING_PY）
RUN_SCRIPT = """
import sys, os, faulthandler
faulthandler.enable(file=sys.stderr)

import json
import qgis
from qgis.core import QgsApplication, QgsProcessingFeedback, QgsProcessingContext

class ProgressFeedback(QgsProcessingFeedback):
    def __init__(self):
        super().__init__()
        self._last_pct = -1
    def setProgress(self, progress):
        pct = int(progress // 10) * 10
        if pct > self._last_pct:
            self._last_pct = pct
            print(f"Progress: {pct}%", flush=True)

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

algorithm = sys.argv[1]
params = json.loads(sys.argv[2])

registry = QgsApplication.processingRegistry()
algo = registry.algorithmById(algorithm)
name = algo.displayName().strip() if algo else ""

if name:
    print(f"Algorithm name: {name}", flush=True)
print(f"Starting algorithm: {algorithm}", flush=True)

# Pre-flight warnings
def warn(title, msgs):
    print(f"Warning: {title}", flush=True)
    for m in msgs:
        print(f"  {m}", flush=True)

if algo is None:
    all_ids = [a.id() for a in registry.algorithms()]
    al = algorithm.lower()
    similar = [a for a in all_ids if al in a.lower() or a.lower() in al][:5]
    hint = "Use QgisDoc tool with action='search' to find the correct ID."
    if similar:
        warn(f"Algorithm '{algorithm}' not found", [f"Did you mean: {', '.join(similar)}", hint])
    else:
        warn(f"Algorithm '{algorithm}' not found", [hint])
else:
    defs = algo.parameterDefinitions()
    expected = {p.name() for p in defs}
    required = set()
    for p in defs:
        try:
            if not (p.flags() & p.FlagOptional):
                required.add(p.name())
        except AttributeError:
            required.add(p.name())
    actual = set(params.keys())
    doc_hint = f"See: QgisDoc tool with action='read', algorithm='{algorithm}'"
    unknown = actual - expected
    if unknown:
        warn(f"Unknown parameter(s) for '{algorithm}'", [
            f"Got: {sorted(unknown)}",
            f"Defined: {sorted(expected)}",
            "These will be ignored by QGIS.",
            doc_hint,
        ])
    missing = required - actual
    if missing:
        warn(f"Missing required parameter(s) for '{algorithm}'", [
            f"Missing: {sorted(missing)}",
            "QGIS may use defaults or fail.",
            doc_hint,
        ])
    # Per-param value check
    ctx = QgsProcessingContext()
    bad = []
    for p in defs:
        if p.name() not in params:
            continue
        try:
            if not p.checkValueIsAcceptable(params[p.name()], ctx):
                bad.append(f"{p.name()} (type={p.type()}): {params[p.name()]!r}")
        except Exception:
            pass
    if bad:
        warn(f"Parameter value(s) may not be acceptable", bad + ["QGIS will validate at run time.", doc_hint])

feedback = ProgressFeedback()
try:
    result = processing.run(algorithm, params, feedback=feedback)
    log = feedback.textLog().strip()
    if log:
        print(log)
    else:
        print(json.dumps(result, ensure_ascii=False, default=str))
except Exception as e:
    log = feedback.textLog().strip()
    if log:
        print(log, flush=True)
    print("Error: " + str(e), file=sys.stderr)
    sys.exit(1)
finally:
    qgs.exitQgis()
"""


def run(args: dict) -> tuple[str, bool]:
    algorithm = args.get("algorithm", "")
    params = args.get("params", {})
    timeout = args.get("timeout", 60)

    if not algorithm:
        return "参数错误: 缺少 algorithm", True

    # 参数验证
    if not isinstance(params, dict):
        return "参数错误: params 必须是字典", True

    if not qgis_bridge.is_available():
        return (
            "QGIS 未安装或未找到。\n"
            "请从 https://qgis.org 下载安装 QGIS，"
            "然后运行 geo_env_status 确认环境就绪。"
        ), True

    logger.info("run_qgis_algorithm: %s timeout=%ds", algorithm, timeout)

    params_json = json.dumps(params, ensure_ascii=False)
    started = time.time()

    try:
        proc = qgis_bridge.run_script(
            RUN_SCRIPT,
            args=[algorithm, params_json],
            timeout=timeout + 10,  # 额外 10s 用于 QGIS 初始化
        )
    except Exception as e:
        elapsed = time.time() - started
        return f"QGIS 子进程异常: {e}\nElapsed: {_format_elapsed(elapsed)}", True

    elapsed = time.time() - started
    elapsed_str = _format_elapsed(elapsed)

    stdout = proc.stdout.strip()
    stderr = proc.stderr.strip()

    if proc.returncode == 0:
        return f"{stdout}\nElapsed: {elapsed_str}", False
    else:
        detail = stderr if stderr else f"exit code {proc.returncode}"
        output = stdout if stdout else "(no output)"
        return f"{output}\n--- stderr ---\n{detail}\nElapsed: {elapsed_str}", True


def _format_elapsed(ms: float) -> str:
    """格式化耗时。"""
    ms = ms * 1000
    if ms < 1000:
        return f"{int(ms)}ms"
    if ms < 60_000:
        return f"{ms / 1000:.1f}s"
    minutes = int(ms / 60_000)
    seconds = int((ms % 60_000) / 1000)
    return f"{minutes}m {seconds}s"
