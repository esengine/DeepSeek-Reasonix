"""sheriff.py — cmd_sheriff, cmd_sheriff_direction"""

from state import load_game, save_game
from roles import alive


def cmd_sheriff_direction(args):
    """警长确定发言起始方向。"""
    s = load_game()
    sheriff = s.get("sheriff")
    if not sheriff:
        print("[!] 当前无警长，不需要指定方向")
        return
    if args.direction not in ("左", "右"):
        print("[!] 方向必须是 左 或 右")
        return
    s["sheriff_direction"] = args.direction
    save_game(s)
    print(f"[警徽] 警长确定从{sheriff}的{args.direction}侧开始发言")


def cmd_sheriff(args):
    """警上竞选。支持退水、PK、自爆后继续。"""
    s = load_game()
    al = alive(s)
    is_pk = args.pk

    candidates = list(args.candidates) if args.candidates else s.get("sheriff_candidates", [])
    withdrew = list(args.withdrew) if args.withdrew else []

    candidates = [c for c in candidates if c not in withdrew and c in al]
    if not candidates:
        print("[警徽] 无人上警（全部退水），警徽流失")
        s["sheriff"] = s["sheriff_candidates"] = None
        save_game(s)
        return

    if withdrew:
        print(f"[警徽] 警上候选人: {', '.join(candidates)}  退水: {', '.join(withdrew)}")
    else:
        print(f"[警徽] 警上候选人: {', '.join(candidates)}")

    if not is_pk and not args.vote:
        print(f"[!] ⚠️ 流程提醒（必须按顺序执行）：")
        print(f"[!] 1. task让每个候选人发言（报查验+警徽流）")
        print(f"[!] 2. parallel_tasks收集警下非候选人投票")
        print(f"[!] 3. 执行: sheriff --candidates {' '.join(candidates)} --vote '投票人:候选人'")
        print(f"[!] 注意：候选人不投票，只有警下玩家投票")

    vote_pairs = [(v, t) for v, t in [x.split(":", 1) for x in (args.vote or []) if ":" in x]]
    tally = {c: 0 for c in candidates}
    for voter, target in vote_pairs:
        if voter in al and target in candidates:
            if is_pk and voter in candidates:
                continue
            tally[target] = tally.get(target, 0) + 1

    voters_in_vote = {v for v, t in vote_pairs}
    candidates_set = set(candidates)
    non_candidates = [n for n in al if n not in candidates_set]
    missing_voters = [n for n in non_candidates if n not in voters_in_vote]
    if missing_voters and args.vote:
        print(f"[!] ⚠️ 警告: {len(missing_voters)}/{len(non_candidates)} 玩家未投票: {', '.join(missing_voters)}")
        print(f"[!] 建议: 如系超时，可 task 单独收票后补加 --vote")

    for c, n in sorted(tally.items(), key=lambda x: -x[1]):
        v = f"  📊 {c}: {n} 票"
        if is_pk: v += " (PK)"
        print(v)

    total = sum(tally.values())
    if total == 0:
        print("[警徽] 无人投票，警徽流失")
        s["sheriff"] = s["sheriff_candidates"] = None
        save_game(s)
        return

    max_votes = max(tally.values())
    winners = [c for c, n in tally.items() if n == max_votes]

    if len(winners) == 1:
        s["sheriff"] = winners[0]
        s["sheriff_candidates"] = []
        print(f"[警徽] {winners[0]} 当选警长！({max_votes}票)")
        s["log"].append(f"警长 {winners[0]} 当选")
    elif len(winners) > 1 and not is_pk:
        s["sheriff_candidates"] = winners
        print(f"[警徽] 平票 ({', '.join(winners)})，进入PK发言")
        print(f"[!] ⚠️ PK流程提醒：")
        print(f"[!] 1. task让PK候选人再次发言")
        print(f"[!] 2. 收集警下投票")
        print(f"[!] 3. 执行: sheriff --candidates {' '.join(winners)} --pk --vote ...")
        s["sheriff"] = None
    else:
        s["sheriff"] = s["sheriff_candidates"] = None
        print(f"[警徽] {'PK再' if is_pk else ''}次平票 ({', '.join(winners)})，警徽流失")
    save_game(s)
