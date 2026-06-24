#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""roles.py — 狼人杀角色定义、配置加载、角色分配"""

import json, random
from pathlib import Path

ROLES = {
    "seer": "预言家", "witch": "女巫", "hunter": "猎人",
    "wolf": "狼人", "villager": "村民",
    "guard": "守卫", "idiot": "白痴",
    "mech_wolf": "机械狼", "wolf_king": "狼王",
}
ROLE_ICONS = {
    "wolf": "[W]", "seer": "[S]", "witch": "[Wi]",
    "hunter": "[H]", "villager": "[V]", "guard": "[G]", "idiot": "[I]",
    "mech_wolf": "[Mw]", "wolf_king": "[Wk]",
}


def is_final_battle(s, cfg=None):
    """检测是否进入决胜局。
    
    决胜局条件：
    1. 狼人数量 ≥ 好人数量（狼人必胜）
    2. 狼人数量 × 2 ≥ 好人数量（局势紧张）
    3. 存活人数 ≤ 4（最后决战）
    
    返回: (is_final: bool, level: str)
        level: "critical" | "tense" | "normal"
    """
    if cfg is None:
        cfg = {}
    
    al = [n for n, p in s["players"].items() if p["alive"]]
    wc = sum(1 for n in al if s["players"][n]["role"] in ("wolf", "mech_wolf"))
    gc = len(al) - wc
    
    # 狼人必胜
    if wc >= gc:
        return True, "critical"
    
    # 局势紧张（8人以下且狼人数量过半时）
    if wc * 2 >= gc and len(al) <= 8:
        return True, "tense"
    
    # 最后决战
    if len(al) <= 4:
        return True, "tense"
    
    return False, "normal"

DEFAULT_CONFIG = {
    # ── 女巫 ──
    "witch_self_save_n1": True,          # 首夜可自救
    # ── 守卫 ──
    "guard_witch_overlap_lethal": False, # 同守同救不死
    # ── 狼队 ──
    "wolf_self_kill": False,             # 狼自刀
    "wolf_explode": True,                # 狼王自爆
    "double_kill_after_explode": False,  # 自爆后双刀
    # ── 猎人 ──
    "hunter_active_shot": False,         # 主动开枪
    # ── 机械狼 ──
    "mechanical_wolf": False,            # 机械狼角色
    "mimic_wolf": False,                 # 机械狼模仿
    # ── 通用 ──
    "reveal_role_on_death": False,       # 死亡翻牌
    "night_kill_last_words": False,      # 夜死遗言
    # ── 角色启用 ──
    "role_seer": True,
    "role_witch": True,
    "role_hunter": True,
    "role_guard": True,
    "role_idiot": True,
    "role_wolf_king": True,              # 狼王
}

ROLE_CONFIG_MAP = {
    "seer": "role_seer", "witch": "role_witch",
    "hunter": "role_hunter", "guard": "role_guard", "idiot": "role_idiot",
}
ALL_GOD_ROLES = ["seer", "witch", "hunter", "guard", "idiot"]

STANDARD_SETUPS = {
    4:  (1, ["seer"], 2),
    5:  (1, ["seer", "witch"], 2),
    6:  (2, ["seer", "witch"], 2),
    7:  (2, ["seer", "witch", "hunter"], 2),
    8:  (2, ["seer", "witch", "hunter"], 3),
    9:  (3, ["seer", "witch", "hunter"], 3),
    10: (3, ["seer", "witch", "hunter", "idiot"], 3),
    11: (3, ["seer", "witch", "hunter", "guard"], 4),
    12: (4, ["seer", "witch", "hunter", "guard"], 4),
}

CONFIG_FILE = "werewolf_config.json"
PROJECT_ROOT = Path(__file__).parent.parent.parent.parent.parent


def load_config():
    """加载配置文件，不存在则创建。"""
    path = PROJECT_ROOT / CONFIG_FILE
    if not path.exists():
        path.write_text(
            json.dumps(DEFAULT_CONFIG, ensure_ascii=False, indent=2),
            encoding="utf-8"
        )
        print(f"[CFG] 已创建默认配置 {CONFIG_FILE}")
    try:
        cfg = json.loads(path.read_text(encoding="utf-8"))
    except (json.JSONDecodeError, OSError) as e:
        print(f"[WARN] 配置文件读取失败，使用默认配置: {e}")
        cfg = {}
    if "guard_witch_overlap_kill" in cfg and "guard_witch_overlap_lethal" not in cfg:
        cfg["guard_witch_overlap_lethal"] = cfg.pop("guard_witch_overlap_kill")
    for k, v in DEFAULT_CONFIG.items():
        cfg.setdefault(k, v)
    return cfg


def alive(s):
    return [n for n, p in s["players"].items() if p["alive"]]


def wolves(s, cfg=None):
    """所有存活的狼人阵营（含机械狼，不含模仿中的机械狼）。"""
    if cfg is None:
        cfg = load_config()
    result = []
    for n, p in s["players"].items():
        if not p["alive"]:
            continue
        if p["role"] == "wolf":
            result.append(n)
        elif p["role"] == "mech_wolf":
            if not cfg.get("mimic_wolf"):
                result.append(n)
    return result


def find_player(s, role):
    return [n for n, p in s["players"].items()
            if p["role"] == role and p["alive"]]


def is_wolf_role(role):
    return role in ("wolf", "mech_wolf")


def pick_roles(player_count, cfg):
    """根据玩家数和配置开关选取身份组合。"""
    if player_count in STANDARD_SETUPS:
        wolf_count, gods, _ = STANDARD_SETUPS[player_count]
    else:
        wolf_count = player_count // 3
        gods = ["seer", "witch", "hunter"][:wolf_count]
    gods = [r for r in gods if cfg.get(ROLE_CONFIG_MAP.get(r, ""), True)]
    while len(gods) < wolf_count and wolf_count > 0:
        extra = [r for r in ALL_GOD_ROLES
                 if r not in gods and cfg.get(ROLE_CONFIG_MAP.get(r, ""), True)]
        if not extra:
            break
        gods.append(extra[0])
    roles = ["wolf"] * wolf_count
    if cfg.get("mechanical_wolf") and wolf_count >= 1:
        for i, r in enumerate(roles):
            if r == "wolf":
                roles[i] = "mech_wolf"
                break
    roles.extend(gods)
    roles.extend(["villager"] * max(0, player_count - len(roles)))
    random.shuffle(roles)
    return roles
