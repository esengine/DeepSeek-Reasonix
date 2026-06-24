"""special.py — cmd_hunter_shot, cmd_explode"""

from state import load_game, save_game, log_event, record_game_result, LOG_FILE, STATS_FILE
from roles import (
    ROLES, ROLE_ICONS, DEFAULT_CONFIG, load_config,
    alive, wolves, find_player, is_wolf_role, pick_roles, is_final_battle,
)
from utils import extract_name


def cmd_hunter_shot(args):
    """猎人主动开枪（需hunter_active_shot开启）。"""
    cfg = load_config()
    if not cfg.get("hunter_active_shot"):
        print("FAILED: 猎人主动开枪未启用 (hunter_active_shot=false)"); return
    s = load_game()
    shooter = args.shooter
    target = args.target
    if shooter not in s["players"] or not s["players"][shooter]["alive"]:
        print(f"FAILED: 猎人 {shooter} 不存在或已死亡"); return
    if s["players"][shooter]["role"] != "hunter":
        print(f"FAILED: {shooter} 不是猎人"); return
    if target not in alive(s):
        print(f"FAILED: 目标 {target} 不存在或已死亡"); return
    if not args.confirmed:
        print(f"[!] 必须先task问猎人{shooter}是否要主动开枪，得到确认后再加 --confirmed")
        print(f"[!] 正确: task问猎人→得回复→hunter-shot {shooter} {target} --confirmed")
        return
    s["players"][shooter]["alive"] = False
    s["players"][target]["alive"] = False
    print(f"[H] 猎人 {shooter} 主动开枪！带走了 {target}")
    s["log"].append(f"猎人 {shooter} 主动开枪带走 {target}")
    save_game(s)


def cmd_explode(args):
    cfg = load_config()
    if not cfg.get("wolf_explode"):
        print("FAILED: 狼自爆规则未启用 (wolf_explode=false)"); return
    s = load_game()
    name = args.name
    if name not in s["players"] or not s["players"][name]["alive"]:
        print(f"FAILED: 玩家 {name} 不存在或已死亡"); return
    if not is_wolf_role(s["players"][name]["role"]):
        print(f"FAILED: {name} 不是狼人，无法自爆"); return
    if not args.confirmed:
        print(f"[!] 必须先task问狼人{name}是否要自爆，得到确认后再加 --confirmed")
        print(f"[!] 正确: task问狼人→得回复→explode {name} --confirmed")
        return
    s["players"][name]["alive"] = False
    print(f"[!] 狼人 {name} 自爆！跳过白天投票，直接进入黑夜")
    s["log"].append(f"狼人 {name} 自爆")
    if cfg.get("double_kill_after_explode") and cfg.get("mechanical_wolf"):
        mech = find_player(s, "mech_wolf")
        if mech:
            s["extra_night_kill"] = True
            print(f"[Mw] 机械狼 {mech[0]} 获得夜间额外击杀能力")
    # 胜负判定：自爆可能导致狼全灭（好人胜）或狼数≥好人（狼胜）
    al2 = alive(s)
    wc = len(wolves(s, cfg))
    if wc == 0:
        print("[G] 好人阵营获胜！所有狼人已被消灭")
        s["phase"], s["winner"] = "ended", "good"
        log_event("game_end", {"winner": "good", "round": s["round"]})
        record_game_result(s)
    elif wc >= len(al2) - wc:
        print(f"[G] 狼人阵营获胜！狼人({wc}) >= 好人({len(al2)-wc})")
        s["phase"], s["winner"] = "ended", "evil"
        log_event("game_end", {"winner": "evil", "round": s["round"]})
        record_game_result(s)
    else:
        s["phase"] = "night"
        s["round"] += 1
    save_game(s)
