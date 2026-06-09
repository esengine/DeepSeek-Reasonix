"""QGIS 子进程桥接 — 共享的 bat-bridge 调用 + 环境构建。

所有 QGIS 工具（run_qgis_algorithm, qgis_doc）通过此模块调用 QGIS Python。
参照 GeoCode qgis-env.ts 的 bat-bridge 模式。
"""

import json
import logging
import os
import re
import subprocess
import tempfile
from pathlib import Path

logger = logging.getLogger(__name__)

# 缓存 QGIS 路径
_qgis_root: str | None = None
_qgis_bat: str | None = None


def _find_qgis() -> tuple[str, str] | None:
    """查找 QGIS 安装根目录和 bat 路径。结果缓存。"""
    global _qgis_root, _qgis_bat
    if _qgis_root and _qgis_bat:
        return _qgis_root, _qgis_bat

    search_dirs = [r"C:\Program Files", r"C:\Program Files (x86)"]
    for base in search_dirs:
        base_path = Path(base)
        if not base_path.is_dir():
            continue
        try:
            for entry in base_path.iterdir():
                if not entry.is_dir():
                    continue
                if "qgis" not in entry.name.lower() and "osgeo" not in entry.name.lower():
                    continue
                for bat_name in ["python-qgis-ltr.bat", "python-qgis.bat"]:
                    bat = entry / "bin" / bat_name
                    if bat.exists():
                        _qgis_root = str(entry)
                        _qgis_bat = str(bat)
                        logger.info("found QGIS at %s, bat=%s", _qgis_root, _qgis_bat)
                        return _qgis_root, _qgis_bat
        except PermissionError:
            continue
    return None


def _python_home(root: str) -> str:
    """推导 PYTHONHOME 路径（apps/PythonNNN）。"""
    apps = Path(root) / "apps"
    if apps.is_dir():
        for entry in sorted(apps.iterdir(), reverse=True):
            if re.match(r"^Python\d+$", entry.name, re.IGNORECASE):
                return str(entry)
    return ""


def run_script(script: str, args: list[str] | None = None, timeout: int = 60) -> subprocess.CompletedProcess:
    """通过 bat-bridge 执行 QGIS Python 脚本。

    自动查找 QGIS 安装、设置 PYTHONHOME、写临时脚本文件。

    Args:
        script: Python 脚本内容
        args: 传给脚本的命令行参数
        timeout: 超时秒数

    Returns:
        subprocess.CompletedProcess
    """
    found = _find_qgis()
    if not found:
        raise RuntimeError(
            "QGIS 未安装或未找到。请从 https://qgis.org 下载安装 QGIS。"
        )
    root, bat = found

    # 写临时脚本
    fd, tmp = tempfile.mkstemp(suffix=".py", prefix="qgis_")
    os.write(fd, script.encode("utf-8"))
    os.close(fd)

    try:
        env = os.environ.copy()
        env["PYTHONHOME"] = _python_home(root)
        env["PYTHONUTF8"] = "1"
        env["PYTHONIOENCODING"] = "utf-8"

        cmd_args = [bat, tmp]
        if args:
            cmd_args.extend(args)
        cmd_line = subprocess.list2cmdline(cmd_args)

        logger.debug("spawning QGIS: %s", cmd_line[:200])
        return subprocess.run(
            cmd_line,
            capture_output=True, text=True, timeout=timeout,
            shell=True, env=env,
        )
    finally:
        try:
            os.unlink(tmp)
        except OSError:
            pass


def run_script_stream(script: str, args: list[str] | None = None, timeout: int = 60):
    """通过 bat-bridge 流式执行 QGIS Python 脚本（生成器）。

    每收到一行 stdout 就 yield，适合长时间运行的算法。
    """
    found = _find_qgis()
    if not found:
        raise RuntimeError("QGIS 未安装或未找到。")

    root, bat = found

    fd, tmp = tempfile.mkstemp(suffix=".py", prefix="qgis_stream_")
    os.write(fd, script.encode("utf-8"))
    os.close(fd)

    env = os.environ.copy()
    env["PYTHONHOME"] = _python_home(root)
    env["PYTHONUTF8"] = "1"
    env["PYTHONIOENCODING"] = "utf-8"

    cmd_args = [bat, tmp]
    if args:
        cmd_args.extend(args)
    cmd_line = subprocess.list2cmdline(cmd_args)

    logger.debug("spawning QGIS stream: %s", cmd_line[:200])
    proc = subprocess.Popen(
        cmd_line,
        stdout=subprocess.PIPE, stderr=subprocess.PIPE,
        text=True, shell=True, env=env,
    )

    try:
        for line in proc.stdout:
            yield line
        proc.wait(timeout=timeout)
    finally:
        proc.terminate()
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            proc.kill()
        try:
            os.unlink(tmp)
        except OSError:
            pass


def is_available() -> bool:
    """检查 QGIS 是否可用。"""
    return _find_qgis() is not None
