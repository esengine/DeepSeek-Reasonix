#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""state.py — 狼人杀状态管理：加载/保存/文件锁/日志/统计"""

import json, os, time, sys
from pathlib import Path

# 项目根目录（从脚本位置往上5级）
# 脚本位置: .reasonix/skills/werewolf/scripts/state.py
# 项目根目录: /c/Users/zhujieling11/goal-test/
PROJECT_ROOT = Path(__file__).parent.parent.parent.parent.parent

GAME_FILE = PROJECT_ROOT / "werewolf_state.json"
LOCK_FILE = PROJECT_ROOT / "werewolf_state.json.lock"
STATS_FILE = PROJECT_ROOT / "werewolf_stats.json"
LOG_FILE = PROJECT_ROOT / "werewolf_log.jsonl"
RAW_LOG_FILE = PROJECT_ROOT / "werewolf_raw_log.jsonl"


def acquire_lock():
    """获取文件锁（跨平台，忙等最多3秒，自动清理僵尸锁）。"""
    lock_path = Path(LOCK_FILE)
    # 检查锁文件是否过期（>10秒）或持有进程已退出
    if lock_path.exists():
        try:
            lock_pid = int(lock_path.read_text().strip())
            # 检查进程是否仍在运行
            os.kill(lock_pid, 0)  # 不发送信号，只检查进程存在性
            # 进程还在运行，锁有效，等待
        except (ValueError, ProcessLookupError, PermissionError):
            # 锁文件损坏或进程已退出，清理僵尸锁
            lock_path.unlink(missing_ok=True)
        except OSError:
            # 其他OS错误，清理锁
            lock_path.unlink(missing_ok=True)
        else:
            # 进程还在运行但锁文件过期>10秒，也清理
            if (time.time() - lock_path.stat().st_mtime) > 10:
                lock_path.unlink(missing_ok=True)
    
    deadline = time.time() + 3
    while time.time() < deadline:
        try:
            fd = os.open(str(lock_path), os.O_CREAT | os.O_EXCL | os.O_WRONLY)
            # 写入当前进程PID
            os.write(fd, str(os.getpid()).encode())
            os.close(fd)
            return True
        except FileExistsError:
            time.sleep(0.05)
    return False


def release_lock():
    Path(LOCK_FILE).unlink(missing_ok=True)


def load_game():
    if not acquire_lock():
        print("[FATAL] 无法获取文件锁，终止操作")
        sys.exit(1)
    try:
        return json.loads(Path(GAME_FILE).read_text(encoding="utf-8"))
    except (json.JSONDecodeError, FileNotFoundError):
        print("[ERROR] 状态文件损坏，已重置")
        Path(GAME_FILE).unlink(missing_ok=True)
        sys.exit(1)
    finally:
        release_lock()


def save_game(s):
    if not acquire_lock():
        print("[FATAL] 无法获取文件锁，终止操作")
        sys.exit(1)
    try:
        Path(GAME_FILE).write_text(
            json.dumps(s, ensure_ascii=False, indent=2), encoding="utf-8"
        )
    finally:
        release_lock()


def _rotate_log(log_path, max_size=10*1024*1024):
    """日志轮转：超过max_size时重命名为.bak，保留最近一份备份。"""
    try:
        if Path(log_path).stat().st_size > max_size:
            bak = Path(log_path).with_suffix(".jsonl.bak")
            if bak.exists():
                bak.unlink()
            Path(log_path).rename(bak)
    except OSError:
        pass


def log_event(event_type, data):
    """写入 JSONL 日志（自动轮转超过10MB的旧日志）。"""
    _rotate_log(LOG_FILE)
    entry = {"t": time.time(), "type": event_type, "data": data}
    try:
        with open(LOG_FILE, "a", encoding="utf-8") as f:
            f.write(json.dumps(entry, ensure_ascii=False) + "\n")
    except OSError as e:
        print(f"[WARN] 日志写入失败: {e}")


def log_raw_event(event_type, player, raw_text, round_num=0):
    """写入 AI 原始回复日志（JSONL，自动轮转超过10MB的旧日志）。"""
    _rotate_log(RAW_LOG_FILE)
    entry = {
        "t": time.time(),
        "type": event_type,
        "player": player,
        "round": round_num,
        "raw": raw_text,
    }
    try:
        with open(RAW_LOG_FILE, "a", encoding="utf-8") as f:
            f.write(json.dumps(entry, ensure_ascii=False) + "\n")
    except Exception:
        pass


def record_game_result(s):
    """记录一局游戏结果到统计文件。"""
    winner = s.get("winner", "unknown")
    player_count = len(s["players"])
    roles = {n: p["role"] for n, p in s["players"].items()}
    al = [n for n, p in s["players"].items() if p["alive"]]
    entry = {
        "time": time.time(), "winner": winner,
        "players": player_count, "rounds": s["round"],
        "roles": roles, "survivors": al,
    }
    try:
        stats = json.loads(Path(STATS_FILE).read_text(encoding="utf-8")) \
            if Path(STATS_FILE).exists() else []
    except Exception:
        stats = []
    stats.append(entry)
    Path(STATS_FILE).write_text(
        json.dumps(stats, ensure_ascii=False, indent=2), encoding="utf-8"
    )
