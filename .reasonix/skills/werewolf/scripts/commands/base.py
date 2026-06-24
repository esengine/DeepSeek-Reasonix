"""base.py — 共享工具函数（死亡处理、胜负判定等）"""

import json
from pathlib import Path

from state import PROJECT_ROOT

GAME_FILE = PROJECT_ROOT / "werewolf_state.json"
CONFIG_FILE = PROJECT_ROOT / "werewolf_config.json"


def process_night_death(s, log_key, target_name, dead_set, overlap_kill):
    """处理一次击杀的死亡判定（守护/女巫救/同守同救）。"""
    if not s["players"][target_name]["alive"]:
        return
    gp = s["players"][target_name].get("guard_protected", False)
    ws = s["players"][target_name].get("witch_saved", False)
    if gp and ws:
        if overlap_kill:
            dead_set.add(target_name)
            print(f"  [同守同救] {target_name} 被守+救仍死亡")
        else:
            print(f"  [同守同救已关] 守卫+女巫共同保护了 {target_name}")
    elif gp or ws:
        pass
    else:
        dead_set.add(target_name)
