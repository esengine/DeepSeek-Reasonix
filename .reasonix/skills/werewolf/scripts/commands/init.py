"""init.py — cmd_init, cmd_reset"""

from pathlib import Path
from state import load_game, save_game, log_event, record_game_result, LOG_FILE, STATS_FILE
from roles import ROLES, ROLE_ICONS, DEFAULT_CONFIG, load_config, pick_roles
from .base import GAME_FILE, CONFIG_FILE


def cmd_init(args):
    cfg = load_config()
    names = args.players
    if len(names) < 6:
        print("FAILED: 至少需要 6 名玩家"); return
    if len(set(names)) != len(names):
        print("FAILED: 玩家名不能重复"); return
    for n in names:
        if not n.strip():
            print("FAILED: 玩家名不能为空"); return
    roles = pick_roles(len(names), cfg)
    s = {"phase": "night", "round": 1, "players": {}, "log": [],
         "sheriff": None, "mimic_wolf_target": None, "version": 3, "max_rounds": 50,
         "vote_round": 0, "vote_tied": None, "wolf_plans": []}
    for n, r in zip(names, roles):
        s["players"][n] = {
            "role": r, "alive": True, "speeches": [], "votes": [],
            "witch_saved": False, "guard_protected": False,
            "poisoned": False, "idiot_revealed": False,
            "mimic_target": None,
            "witch_antidote_used": False,
            "witch_poison_used": False,
            "can_hunter_shoot": True,
            "mimic_antidote_used": False,
            "mimic_poison_used": False,
            "wolf_plans": [],
        }
    save_game(s)
    print(f"===== 第 1 晚 =====")
    print(f"存活: {len(names)} 人")
    for n in names:
        print(f"  {ROLE_ICONS[s['players'][n]['role']]} {n} 身份: {ROLES[s['players'][n]['role']]}")
    enabled = [k for k, v in cfg.items() if v and k.startswith(("role_", "wolf_", "witch_"))]
    if cfg.get("mechanical_wolf"): enabled.append("mechanical_wolf")
    if cfg.get("mimic_wolf"): enabled.append("mimic_wolf")
    if cfg.get("double_kill_after_explode"): enabled.append("double_kill")
    if enabled:
        print(f"规则: {', '.join(sorted(enabled))}")
    print()


def cmd_reset(args):
    Path(GAME_FILE).unlink(missing_ok=True)
    Path(LOG_FILE).unlink(missing_ok=True)
    # 清理临时文件目录（防止跨局污染）
    from state import PROJECT_ROOT
    wolf_kill_dir = PROJECT_ROOT / "home" / "user" / ".wolf_kill"
    if wolf_kill_dir.exists():
        import shutil
        shutil.rmtree(wolf_kill_dir, ignore_errors=True)
        print(f"OK: 已清理临时文件 {wolf_kill_dir}")
    print("OK: 游戏已重置")
