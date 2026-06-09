"""run_gee_script — 执行 GEE Python 脚本。

移植自 GeoCode gee-run.ts。
使用当前 conda gee 环境的 Python 执行脚本，注入 geocode.py 辅助模块。
"""

import logging
import os
import subprocess
import sys
import time
from pathlib import Path

from .. import config

logger = logging.getLogger(__name__)

# geocode.py helper 所在目录，注入 PYTHONPATH
_HELPER_DIR = str(Path(__file__).parent)

IDLE_TIMEOUT = 60  # 无输出超时秒数
MAX_OUTPUT_LENGTH = 30_000


def _check_gee() -> tuple[bool, str]:
    """检查 GEE 环境是否就绪。返回 (ok, reason)。"""
    try:
        import ee
    except ImportError:
        return False, "earthengine-api 未安装。pip install earthengine-api"

    try:
        from ee import oauth
        cred_path = oauth.get_credentials_path()
        if not cred_path or not os.path.exists(cred_path):
            return False, "GEE 未认证。在终端执行: earthengine authenticate"
    except Exception:
        pass

    try:
        project = config.get_gee_project() or os.environ.get("GEE_PROJECT")
        ee.Initialize(project=project)
        return True, f"GEE ready ({ee.__version__})"
    except Exception as e:
        return False, f"GEE Initialize 失败: {e}"


def _format_elapsed(ms: float) -> str:
    ms = ms * 1000
    if ms < 1000:
        return f"{int(ms)}ms"
    if ms < 60_000:
        return f"{ms / 1000:.1f}s"
    minutes = int(ms / 60_000)
    seconds = int((ms % 60_000) / 1000)
    return f"{minutes}m {seconds}s"


def run(args: dict) -> tuple[str, bool]:
    script = args.get("script", "")
    script_path = args.get("script_path", "")

    if not script and not script_path:
        return "参数错误: 必须提供 script 或 script_path", True
    if script and script_path:
        return "参数错误: script 和 script_path 只能提供一个", True
    if script_path and not os.path.exists(script_path):
        return f"脚本文件不存在: {script_path}", True

    ok, reason = _check_gee()
    if not ok:
        return f"GEE 环境不可用:\n{reason}", True

    mode = "file" if script_path else "inline"
    logger.info("run_gee_script: mode=%s", mode)

    env = os.environ.copy()
    env["PYTHONFAULTHANDLER"] = "1"
    env["PYTHONUTF8"] = "1"
    env["PYTHONIOENCODING"] = "utf-8"
    # 注入 GEE project（优先用凭据默认值，有配置则覆盖）
    project = config.get_gee_project()
    if project:
        env["GEE_PROJECT"] = project

    # 注入 geocode.py helper
    existing = env.get("PYTHONPATH", "")
    sep = os.pathsep
    env["PYTHONPATH"] = _HELPER_DIR + (sep + existing if existing else "")

    python_exe = sys.executable
    cmd_args = [python_exe, script_path] if script_path else [python_exe, "-c", script]

    started = time.time()
    output_lines = []
    output_total = 0

    try:
        proc = subprocess.Popen(
            cmd_args,
            stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            text=True, env=env,
        )

        last_output = time.time()
        timed_out = False
        stderr_data = ""

        while proc.poll() is None:
            if time.time() - last_output > IDLE_TIMEOUT:
                proc.terminate()
                timed_out = True
                break

            line = proc.stdout.readline()
            if line:
                output_lines.append(line)
                output_total += len(line)
                last_output = time.time()
            else:
                time.sleep(0.1)

        remaining, stderr_data = proc.communicate(timeout=5)
        if remaining:
            output_lines.append(remaining)
            output_total += len(remaining)

    except subprocess.TimeoutExpired:
        proc.kill()
        elapsed = time.time() - started
        return f"GEE 脚本执行超时\nElapsed: {_format_elapsed(elapsed)}", True

    elapsed = time.time() - started
    elapsed_str = _format_elapsed(elapsed)

    stdout = "".join(output_lines)
    if output_total > MAX_OUTPUT_LENGTH:
        stdout = stdout[:MAX_OUTPUT_LENGTH] + "\n\n... (output truncated)"

    stderr = stderr_data or ""

    if timed_out:
        banner = (
            f"--- idle timeout ---\n"
            f"No output for {IDLE_TIMEOUT}s. "
            f"Use heartbeat() in long-running operations."
        )
        output = f"{stdout}\n\n{banner}\nElapsed: {elapsed_str}" if stdout.strip() else f"{banner}\nElapsed: {elapsed_str}"
        return output, True

    if proc.returncode != 0:
        detail = stderr.strip() if stderr.strip() else f"exit code {proc.returncode}"
        output = stdout if stdout.strip() else "(no output)"
        return f"{output}\n--- stderr ---\n{detail}\nElapsed: {elapsed_str}", True

    return f"{stdout}Elapsed: {elapsed_str}", False
