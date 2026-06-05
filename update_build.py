#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
DeepSeek-Reasonix 自动更新 & 编译脚本 (Windows)

功能：
  1. 拉取 main-v2 分支最新源码
  2. 编译 desktop production build

用法：
  python update_build.py
"""

import sys
import subprocess
import shutil
import time
from pathlib import Path

# ============================================================
# 配置
# ============================================================
PROJECT_ROOT = Path(r"C:\Users\Administrator\DeepSeek-Reasonix")
DESKTOP_DIR = PROJECT_ROOT / "desktop"
BUILD_OUTPUT = DESKTOP_DIR / "build" / "bin" / "reasonix-desktop.exe"
BRANCH = "main-v2"
REMOTE = "origin"
PROXY = "http://127.0.0.1:2801"

# ============================================================
# 工具函数
# ============================================================

def run(cmd, cwd=None, check=True):
    """执行命令并打印输出"""
    if isinstance(cmd, str):
        cmd_str = cmd
    else:
        cmd_str = " ".join(cmd)
    print(f"\n>>> {cmd_str}")
    # 设置代理环境变量，确保 git 能访问 GitHub
    env = dict(subprocess.os.environ)
    env["HTTP_PROXY"] = PROXY
    env["HTTPS_PROXY"] = PROXY
    return subprocess.run(
        cmd, cwd=cwd, shell=isinstance(cmd, str),
        capture_output=False, check=check, env=env
    )


# ============================================================
# 步骤 1: 拉取最新源码
# ============================================================

def step_fetch():
    print("\n" + "=" * 60)
    print("步骤 1: 拉取最新源码")
    print("=" * 60)

    run(f"git fetch {REMOTE} {BRANCH}", cwd=PROJECT_ROOT)
    run(f"git reset --hard {REMOTE}/{BRANCH}", cwd=PROJECT_ROOT)

    result = subprocess.run(
        "git log --oneline -3", cwd=PROJECT_ROOT,
        capture_output=True, text=True, shell=True
    )
    print(result.stdout)


# ============================================================
# 步骤 2: 编译
# ============================================================

def step_build():
    print("\n" + "=" * 60)
    print("步骤 2: 编译 desktop production build")
    print("=" * 60)

    # 杀掉可能正在运行的旧进程
    if BUILD_OUTPUT.exists():
        print("  检查是否有运行中的实例...")
        subprocess.run(
            'taskkill /F /IM reasonix-desktop.exe /T 2>nul',
            shell=True, capture_output=True
        )
        time.sleep(1)
        try:
            BUILD_OUTPUT.unlink()
            print("  已删除旧产物")
        except PermissionError:
            try:
                BUILD_OUTPUT.rename(BUILD_OUTPUT.with_suffix(".exe.old"))
            except Exception:
                pass

    run("wails build", cwd=DESKTOP_DIR)

    if BUILD_OUTPUT.exists():
        size_mb = BUILD_OUTPUT.stat().st_size / (1024 * 1024)
        print(f"\n  OK 编译成功！")
        print(f"     产物: {BUILD_OUTPUT}")
        print(f"     大小: {size_mb:.1f} MB")
    else:
        print(f"\n  FAIL 编译失败，未找到产物: {BUILD_OUTPUT}")
        sys.exit(1)


# ============================================================
# 主流程
# ============================================================

def main():
    print("=" * 60)
    print(" DeepSeek-Reasonix 自动更新 & 编译")
    print("=" * 60)
    print(f" 项目路径: {PROJECT_ROOT}")
    print(f" 分支:     {BRANCH}")

    if not (PROJECT_ROOT / ".git").exists():
        print("ERROR: 不是 git 仓库")
        sys.exit(1)

    for tool in ["git", "wails", "go", "pnpm"]:
        if shutil.which(tool) is None:
            print(f"ERROR: 未找到 {tool}，请先安装")
            sys.exit(1)

    step_fetch()
    step_build()

    print("\n" + "=" * 60)
    print(" 全部完成！")
    print("=" * 60)


if __name__ == "__main__":
    main()
