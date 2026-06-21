#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""狼人杀游戏引擎 — 全部通过命令行参数驱动，无需交互式 stdin"""
import io, sys, json, random, os, argparse
from pathlib import Path
sys.stdout = io.TextIOWrapper(sys.stdout.buffer, encoding='utf-8')

GAME_FILE = "werewolf_state.json"
ROLES = {"seer": "预言家", "witch": "女巫", "hunter": "猎人", "wolf": "狼人", "villager": "村民"}
ROLE_ICONS = {"wolf": "[W]", "seer": "[S]", "witch": "[Wi]", "hunter": "[H]", "villager": "[V]"}

def load_game():
    return json.loads(Path(GAME_FILE).read_text(encoding="utf-8"))

def save_game(s):
    Path(GAME_FILE).write_text(json.dumps(s, ensure_ascii=False, indent=2), encoding="utf-8")

def alive(s):
    return [n for n, p in s["players"].items() if p["alive"]]
def wolves(s):
    return [n for n, p in s["players"].items() if p["role"] == "wolf" and p["alive"]]
def find_player(s, role):
    return [n for n, p in s["players"].items() if p["role"] == role and p["alive"]]

def cmd_init(args):
    names = args.players
    if len(names) < 6:
        print("FAILED: 至少需要 6 名玩家"); return
    roles = []
    roles.extend(["wolf"] * (len(names) // 3))
    for r in ["seer", "witch", "hunter"]:
        roles.append(r)
    while len(roles) < len(names):
        roles.append("villager")
    random.shuffle(roles)
    s = {"phase": "night", "round": 1, "players": {}, "log": []}
    for n, r in zip(names, roles):
        s["players"][n] = {"role": r, "alive": True, "speeches": [], "votes": [], "protected": False}
    save_game(s)
    # 显示角色
    print(f"===== 第 1 晚 =====")
    print(f"存活: {len(names)} 人")
    for n in names:
        print(f"  {ROLE_ICONS[s['players'][n]['role']]} {n} 身份: {ROLES[s['players'][n]['role']]}")
    print(f"\n用法: python3 werewolf_game.py night --kill <目标> [--check <目标>] [--save] [--poison <目标>]")

def cmd_night(args):
    s = load_game()
    s["phase"] = "night"
    s["log"] = []
    rnd = s["round"]

    # 狼人
    wl = wolves(s)
    if wl and args.kill:
        t = args.kill
        print(f"[W] 狼人 {', '.join(wl)} 决定刀 {t}")
        s["log"].append(f"狼人刀了 {t}")

    # 预言家
    for seer in find_player(s, "seer"):
        if args.check and args.check in s["players"]:
            role = s["players"][args.check]["role"]
            print(f"[S] 预言家查验 {args.check} = {ROLES[role]}")
            s["log"].append(f"预言家查验 {args.check} = {role}")

    # 女巫
    witch = find_player(s, "witch")
    if witch:
        killed = None
        for e in reversed(s["log"]):
            if e.startswith("狼人刀了"):
                killed = e.replace("狼人刀了 ", "")
                break
        if killed and killed != witch[0]:
            if args.save:
                print(f"[Wi] 女巫救了 {killed}")
                s["log"].append(f"女巫救了 {killed}")
                s["players"][killed]["protected"] = True
            elif args.poison:
                print(f"[Wi] 女巫毒了 {args.poison}")
                s["log"].append(f"女巫毒了 {args.poison}")
                s["players"][args.poison]["poisoned"] = True

    # 夜间死亡
    dead = set()
    for e in s["log"]:
        if e.startswith("狼人刀了"):
            t = e.replace("狼人刀了 ", "")
            if not s["players"][t].get("protected"):
                dead.add(t)
    for n, p in s["players"].items():
        if p.get("poisoned") and p["alive"]:
            dead.add(n)
    for n in dead:
        s["players"][n]["alive"] = False

    if dead:
        print(f"[X] 昨夜死亡: {', '.join(sorted(dead))}")
        s["log"].append(f"昨夜死亡: {', '.join(sorted(dead))}")
    else:
        print(f"[M] 昨夜平安")
        s["log"].append("昨夜平安")

    s["phase"] = "day"
    save_game(s)

def cmd_day(args):
    s = load_game()
    al = alive(s)
    print(f"===== 第 {s['round']} 天 =====")
    print(f"存活: {len(al)} 人 = {' '.join(al)}")

    # 发言
    if args.speeches:
        for sp in args.speeches:
            if ":" in sp:
                name, speech = sp.split(":", 1)
                if name in s["players"]:
                    s["players"][name]["speeches"].append(f"Day{s['round']}: {speech}")
                    print(f"  {name}: {speech}")

    # 投票
    s["phase"] = "vote"
    votes = {}
    if args.votes:
        for v in args.votes:
            if ":" in v:
                voter, target = v.split(":", 1)
                if voter in al and target in al:
                    votes[target] = votes.get(target, 0) + 1
                    s["players"][voter]["votes"].append(target)

    if votes:
        max_v = max(votes.values())
        executed = [n for n, v in votes.items() if v == max_v]
        if len(executed) == 1:
            t = executed[0]
            s["players"][t]["alive"] = False
            print(f"[J] {t} 被处决 ({votes[t]} 票)")
            s["log"].append(f"处决了 {t}")
            # 猎人
            if s["players"][t]["role"] == "hunter":
                if args.hunter_target and args.hunter_target in alive(s):
                    s["players"][args.hunter_target]["alive"] = False
                    print(f"[H] 猎人带走了 {args.hunter_target}")
        else:
            print(f"[J] 平票 ({' '.join(executed)})，无人被处决")

    # 检查胜负
    al2 = alive(s)
    wc = len(wolves(s))
    if wc == 0:
        print("[G] 好人阵营获胜！所有狼人已被消灭")
        s["phase"], s["winner"] = "ended", "good"
    elif wc >= len(al2) - wc:
        print(f"[G] 狼人阵营获胜！狼人({wc}) >= 好人({len(al2)-wc})")
        s["phase"], s["winner"] = "ended", "evil"
    else:
        s["round"] += 1
        s["phase"] = "night"
    save_game(s)

def cmd_status(args):
    s = load_game()
    al = alive(s)
    print(f"回合: {s['round']} | {'已结束' if s.get('winner') else s['phase']}")
    if s.get("winner"):
        print(f"胜利: {'好人' if s['winner']=='good' else '狼人'}")
    print(f"存活: {len(al)} 人")
    for n in sorted(s["players"], key=lambda x: s["players"][x]["role"]):
        p = s["players"][n]
        st = "[O]" if p["alive"] else "[X]"
        print(f"  {st} {n} -> {ROLES[p['role']]}")
    print(f"\n用法: python3 werewolf_game.py night --kill <目标> [--check <目标>] [--save|--poison <目标>]")

def cmd_reset(args):
    import pathlib
    pathlib.Path(GAME_FILE).unlink(missing_ok=True)
    print("OK: 游戏已重置")

if __name__ == "__main__":
    import pathlib
    parser = argparse.ArgumentParser(description="狼人杀游戏引擎")
    sub = parser.add_subparsers(dest="cmd")

    p_init = sub.add_parser("init")
    p_init.add_argument("players", nargs="+")

    p_night = sub.add_parser("night")
    p_night.add_argument("--kill")
    p_night.add_argument("--check")
    p_night.add_argument("--save", action="store_true")
    p_night.add_argument("--poison")

    p_day = sub.add_parser("day")
    p_day.add_argument("--speech", nargs="*", default=[])
    p_day.add_argument("--vote", nargs="*", default=[])
    p_day.add_argument("--hunter-target")

    p_status = sub.add_parser("status")
    p_reset = sub.add_parser("reset")

    args = parser.parse_args()
    if args.cmd == "init":
        cmd_init(args)
    elif args.cmd == "night":
        cmd_night(args)
    elif args.cmd == "day":
        cmd_day(args)
    elif args.cmd == "status":
        cmd_status(args)
    elif args.cmd == "reset":
        cmd_reset(args)
    else:
        parser.print_help()
