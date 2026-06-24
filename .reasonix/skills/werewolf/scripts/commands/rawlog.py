"""rawlog.py — cmd_log_raw: 记录 AI 原始回复到 werewolf_raw_log.jsonl"""

import json
from pathlib import Path

from state import log_raw_event, PROJECT_ROOT


def cmd_log_raw(args):
    """记录 AI 玩家的原始回复到 werewolf_raw_log.jsonl"""
    event_type = args.type
    player = args.player
    raw_text = args.text

    # 尝试获取当前轮次（从状态文件直接读取，不经过锁机制）
    round_num = 0
    state_path = PROJECT_ROOT / "werewolf_state.json"
    try:
        if state_path.exists():
            data = json.loads(state_path.read_text(encoding="utf-8"))
            round_num = data.get("round", 0)
    except Exception:
        pass

    log_raw_event(event_type, player, raw_text, round_num)
    print(f"[RAW] 已记录 {player} 的 {event_type} 原始回复")
