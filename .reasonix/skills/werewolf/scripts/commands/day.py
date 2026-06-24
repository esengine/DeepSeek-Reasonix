"""day.py — cmd_day, cmd_day_auto, cmd_vote"""

from state import load_game, save_game, log_event, record_game_result, LOG_FILE, STATS_FILE
from roles import (
    ROLES, ROLE_ICONS, DEFAULT_CONFIG, load_config,
    alive, wolves, find_player, is_wolf_role, pick_roles, is_final_battle,
)
from utils import extract_name, extract_votes_from_responses, extract_witch_action
from game_flow import validate_day_steps, mark_step_completed


def cmd_vote(args):
    """独立投票命令。接收AI回复，展示每人投票，统计并执行。"""
    cfg = load_config()
    s = load_game()
    al = alive(s)
    all_players = list(s["players"].keys())

    round_speeches = sum(1 for p in s["players"].values() for sp in p.get("speeches", []) if f"Day{s['round']}:" in sp)
    if round_speeches < len(al) and not args.no_speech:
        print(f"[!] 本轮只有 {round_speeches}/{len(al)} 人发言，建议补完发言再投票")
        print(f"[!] 如果确认不发言，请加 --no-speech")

    sheriff_dir = s.get("sheriff_direction") or getattr(args, 'start_from', None)
    if s.get("sheriff") and s["players"][s["sheriff"]]["alive"] and not sheriff_dir:
        print(f"[!] 警长 {s['sheriff']} 还在！必须先确定发言方向")
        print(f"[!] 方案A: task问警长→sheriff-direction 左|右→再执行本命令")
        print(f"[!] 方案B: task问警长→vote --start-from 左|右")
        return

    vote_pairs = extract_votes_from_responses(args.vote or [], all_players)
    print(f"===== 投票结果 =====")

    votes = {}
    for voter, target in vote_pairs:
        voter_alive = voter in alive(s)
        voter_can_vote = not s["players"].get(voter, {}).get("idiot_revealed", False)
        if voter_alive and voter_can_vote and target in alive(s):
            weight = 1.5 if s.get("sheriff") == voter else 1.0
            votes[target] = votes.get(target, 0) + weight
            s["players"][voter]["votes"].append(target)
            w = f"(×{weight})" if weight != 1.0 else ""
            print(f"  {voter} → {target}{w}")

    if not votes:
        print("  (无人投票)")
        return

    print(f"  {'─'*20}")
    for t, c in sorted(votes.items(), key=lambda x: -x[1]):
        print(f"  📊 {t}: {c} 票")

    max_v = max(votes.values())
    executed = [n for n, v in votes.items() if v == max_v]

    if len(executed) == 1:
        t = executed[0]
        s["vote_round"] = 0
        s["vote_tied"] = None
        if s["players"][t]["role"] == "idiot" and not s["players"][t].get("idiot_revealed"):
            if args.idiot_reveal:
                s["players"][t]["idiot_revealed"] = True
                print(f"[J] {t} 被处决 — 但他是白痴！翻牌不死")
                s["log"].append(f"白痴 {t} 翻牌")
            else:
                s["players"][t]["alive"] = False
                print(f"[J] {t} 被处决 ({votes[t]} 票)")
                s["log"].append(f"处决了 {t}")
                if args.last_words:
                    for lw in args.last_words:
                        if ":" in lw:
                            name, text = lw.split(":", 1)
                            if name == t:
                                s["players"][name]["last_words"] = text
                                print(f"  💬 遗言 [{name}]: {text}")
        else:
            s["players"][t]["alive"] = False
            print(f"[J] {t} 被处决 ({votes[t]} 票)")
            s["log"].append(f"处决了 {t}")
            if args.last_words:
                for lw in args.last_words:
                    if ":" in lw:
                        name, text = lw.split(":", 1)
                        if name == t:
                            s["players"][name]["last_words"] = text
                            print(f"  💬 遗言 [{name}]: {text}")
            if s["players"][t]["role"] == "hunter" and s["players"][t].get("can_hunter_shoot", True):
                hunter_t = extract_name(args.hunter or "", alive(s)) if args.hunter else None
                if hunter_t and hunter_t in alive(s):
                    s["players"][hunter_t]["alive"] = False
                    print(f"[H] 猎人带走了 {hunter_t}")
            if cfg.get("mechanical_wolf") and not cfg.get("mimic_wolf") and s["players"][t]["role"] == "mech_wolf":
                mw_t = extract_name(args.mechwolf_target or "", alive(s)) if getattr(args, 'mechwolf_target', None) else None
                if mw_t and mw_t in alive(s):
                    s["players"][mw_t]["alive"] = False
                    print(f"[Mw] 机械狼被处决，带走了 {mw_t}")
            if s["players"][t]["role"] == "white_wolf_king" and cfg.get("role_white_wolf_king"):
                ww_targets = getattr(args, 'white_wolf_targets', None) or []
                if len(ww_targets) < 2:
                    print(f"[!] 白狼王 {t} 被处决！需带两人，但只提供了 {len(ww_targets)} 个目标")
                    print(f"[!] 正确: day-auto 或 vote 加 --white-wolf 目标1 目标2")
                else:
                    for ww_t in ww_targets[:2]:
                        if ww_t in alive(s):
                            s["players"][ww_t]["alive"] = False
                            print(f"[WW] 白狼王带走了 {ww_t}")
                            s["log"].append(f"白狼王 {t} 被处决带走了 {ww_t}")
                    if not s.get("witch_poison_used"):
                        pass  # 不需要额外处理
    else:
        print(f"[J] 平票 ({' '.join(executed)})")
        if not s.get("vote_round"):
            s["vote_round"] = 2
            s["vote_tied"] = executed
            print(f"[J] 进入再投，仅限 {' '.join(executed)}")
        else:
            s["vote_round"] = 0
            s["vote_tied"] = None
            print(f"[J] 再投平票，无人出局")

    is_final, final_level = is_final_battle(s, cfg)
    if is_final:
        if final_level == "critical":
            print(f"[!] 🔴 决胜局！狼人数量 ≥ 好人数量，好人投错就输！")
        else:
            print(f"[!] ⚠️ 决胜局！狼人数量接近好人，局势紧张！")

    al2 = alive(s)
    wc = len(wolves(s, cfg))
    if wc == 0:
        print("[G] 好人阵营获胜！")
        s["phase"], s["winner"] = "ended", "good"
        log_event("game_end", {"winner": "good", "round": s["round"]})
        record_game_result(s)
    elif wc >= len(al2) - wc:
        print(f"[G] 狼人阵营获胜！")
        s["phase"], s["winner"] = "ended", "evil"
        log_event("game_end", {"winner": "evil", "round": s["round"]})
        record_game_result(s)
    elif s.get("vote_round"):
        s["phase"] = "vote"
    else:
        s["round"] += 1
        s["phase"] = "night"
    save_game(s)


def cmd_day(args):
    cfg = load_config()
    s = load_game()
    al = alive(s)
    print(f"===== 第 {s['round']} 天 =====")
    print(f"存活: {len(al)} 人 = {' '.join(al)}")

    # 先处理发言数据（即使第1天需要警长竞选）
    speeches = args.speech or []
    if speeches:
        for sp in speeches:
            if ":" in sp:
                name, speech = sp.split(":", 1)
                if name in s["players"]:
                    s["players"][name]["speeches"].append(f"Day{s['round']}: {speech}")
                    log_event("speech", {"round": s["round"], "name": name, "text": speech.strip()})
                    print(f"  {name}: {speech}")

    if s["round"] == 1 and not s.get("sheriff") and not getattr(args, 'no_sheriff', False):
        print(f"[!] ⚠️ 第1天必须先进行警长竞选！")
        print(f"[!] 流程提醒：")
        print(f"[!] 1. parallel_tasks问每个存活玩家是否上警")
        print(f"[!] 2. 收集上警候选人")
        print(f"[!] 3. task让候选人发言（报查验+警徽流）")
        print(f"[!] 4. parallel_tasks收集警下投票")
        print(f"[!] 5. 执行: sheriff --candidates 候选人 --vote '投票人:候选人'")
        print(f"[!] 或确认不选警长: day --no-sheriff")
        save_game(s)  # 保存已收集的发言数据
        return

    if speeches:
        speakers = {sp.split(":")[0] for sp in speeches if ":" in sp}
        missing_speakers = [n for n in al if n not in speakers]
        if missing_speakers:
            print(f"[!] ⚠️ 警告: {len(missing_speakers)}/{len(al)} 玩家未发言: {', '.join(missing_speakers)}")
            print(f"[!] 建议: 如系超时，可 task 单独收发言后补加 --speech")

    s["phase"] = "vote"

    sheriff_dir = s.get("sheriff_direction") or getattr(args, 'start_from', None)
    if s.get("sheriff") and s["players"][s["sheriff"]]["alive"] and not sheriff_dir:
        print(f"[!] 警长 {s['sheriff']} 还在！必须先确定发言方向")
        print(f"[!] 方案A: task问警长→sheriff-direction 左|右→再执行本命令")
        print(f"[!] 方案B: task问警长→day --start-from 左|右")
        return

    votes = {}
    vote_list = args.vote or []
    if vote_list:
        for v in vote_list:
            if ":" in v:
                voter, target = v.split(":", 1)
                voter_alive = voter in alive(s)
                voter_can_vote = not s["players"].get(voter, {}).get("idiot_revealed", False)
                if voter_alive and voter_can_vote and target in alive(s):
                    if s.get("vote_tied") and target not in s["vote_tied"]:
                        continue
                    weight = 1.5 if s.get("sheriff") == voter else 1.0
                    votes[target] = votes.get(target, 0) + weight
                    s["players"][voter]["votes"].append(target)
                    log_event("vote", {"round": s["round"], "name": voter, "target": target})

    if votes:
        for t, c in sorted(votes.items(), key=lambda x: -x[1]):
            print(f"  📊 {t}: {c} 票")
        max_v = max(votes.values())
        executed = [n for n, v in votes.items() if v == max_v]
        if len(executed) == 1:
            t = executed[0]
            s["vote_round"] = 0
            s["vote_tied"] = None
            if s["players"][t]["role"] == "idiot" and not s["players"][t].get("idiot_revealed"):
                if args.idiot_reveal:
                    s["players"][t]["idiot_revealed"] = True
                    print(f"[J] {t} 被处决 — 但他是白痴！翻牌不死，失去投票权")
                    s["log"].append(f"白痴 {t} 翻牌")
                else:
                    s["players"][t]["alive"] = False
                    role_info = f"({ROLES[s['players'][t]['role']]})" if cfg.get("reveal_role_on_death") else ""
                    print(f"[J] {t} 被处决 {role_info}({votes[t]} 票)")
                    s["log"].append(f"处决了 {t}")
                    if args.last_words:
                        for lw in args.last_words:
                            if ":" in lw:
                                name, text = lw.split(":", 1)
                                if name == t:
                                    s["players"][name]["last_words"] = text
                                    print(f"  💬 遗言 [{name}]: {text}")
            else:
                s["players"][t]["alive"] = False
                role_info = f"({ROLES[s['players'][t]['role']]})" if cfg.get("reveal_role_on_death") else ""
                print(f"[J] {t} 被处决 {role_info}({votes[t]} 票)")
                s["log"].append(f"处决了 {t}")
                if args.last_words:
                    for lw in args.last_words:
                        if ":" in lw:
                            name, text = lw.split(":", 1)
                            if name == t:
                                s["players"][name]["last_words"] = text
                                print(f"  💬 遗言 [{name}]: {text}")
                if s["players"][t]["role"] == "hunter" and s["players"][t].get("can_hunter_shoot", True):
                    if args.hunter_target and args.hunter_target in alive(s):
                        s["players"][args.hunter_target]["alive"] = False
                        print(f"[H] 猎人带走了 {args.hunter_target}")
                if cfg.get("mechanical_wolf") and not cfg.get("mimic_wolf") and s["players"][t]["role"] == "mech_wolf":
                    if args.mechwolf_target and args.mechwolf_target in alive(s):
                        s["players"][args.mechwolf_target]["alive"] = False
                        print(f"[Mw] 机械狼被处决，带走了 {args.mechwolf_target}")
                if s["players"][t]["role"] == "white_wolf_king" and cfg.get("role_white_wolf_king"):
                    ww_targets = getattr(args, 'white_wolf_targets', None) or getattr(args, 'white_wolf_target', None) or []
                    if len(ww_targets) < 2:
                        print(f"[!] 白狼王 {t} 被处决！需带两人，但只提供了 {len(ww_targets)} 个目标")
                        print(f"[!] 正确: vote 加 --white-wolf 目标1 目标2")
                    else:
                        for ww_t in ww_targets[:2]:
                            if ww_t in alive(s):
                                s["players"][ww_t]["alive"] = False
                                print(f"[WW] 白狼王带走了 {ww_t}")
                                s["log"].append(f"白狼王 {t} 被处决带走了 {ww_t}")
        else:
            print(f"[J] 平票 ({' '.join(executed)})，")
            if not s.get("vote_round"):
                s["vote_round"] = 2
                s["vote_tied"] = executed
                print(f"[J] 进入再投，仅限 {' '.join(executed)} 两票PK")
                print(f"[J] 请使用 day-auto --vote 重新投票")
            else:
                s["vote_round"] = 0
                s["vote_tied"] = None
                print(f"[J] 再投平票，无人被处决")

    sheriff = s.get("sheriff")
    if sheriff and not s["players"].get(sheriff, {}).get("alive", True):
        if not args.pass_sheriff and not args.no_sheriff_confirm:
            print(f"[!] 警长 {sheriff} 死亡且未传位，警徽流失")
            print(f"[!] 下次请用 --pass-sheriff 名字 传位")
        if args.pass_sheriff and args.pass_sheriff in alive(s):
            s["sheriff"] = args.pass_sheriff
            print(f"[警徽] 前警长 {sheriff} 将警徽传给 {args.pass_sheriff}")
            s["log"].append(f"警徽传给 {args.pass_sheriff}")
        elif args.pass_sheriff:
            print(f"[!] 警长传位失败：{args.pass_sheriff} 已死亡")
            s["sheriff"] = None
        else:
            s["sheriff"] = None
            print(f"[警徽] 警长 {sheriff} 死亡，警徽流失")

    al2 = alive(s)
    wc = len(wolves(s, cfg))

    is_final, final_level = is_final_battle(s, cfg)
    if is_final and wc > 0:
        if final_level == "critical":
            print(f"[!] 🔴 决胜局！狼人数量 ≥ 好人数量，好人投错就输！")
        else:
            print(f"[!] ⚠️ 决胜局！狼人数量接近好人，局势紧张！")

    if wc == 0:
        print("[G] 好人阵营获胜！所有狼人已被消灭")
        s["phase"], s["winner"] = "ended", "good"
        log_event("game_end", {"winner": "good", "round": s["round"]})
        record_game_result(s)
    elif wc >= len(al2) - wc:
        print(f"[G] 狼人阵营获胜！狼人({wc}) >= 好人({len(al2)-wc})")
        s["phase"], s["winner"] = "ended", "evil"
        record_game_result(s)
    else:
        max_r = s.get("max_rounds", 50)
        if s["round"] >= max_r:
            print(f"[G] 达到最大轮次({max_r})，平局")
            s["phase"], s["winner"] = "ended", "draw"
            record_game_result(s)
        else:
            s["round"] += 1
            s["phase"] = "night"
    save_game(s)


def cmd_day_auto(args):
    cfg = load_config()
    s = load_game()
    al = alive(s)

    # 验证白天必需步骤是否已完成
    issues = validate_day_steps(s)
    if issues:
        print(f"[!] ❌ 错误: 白天流程未完成，不能执行day-auto")
        for issue in issues:
            print(f"  [!] {issue}")
        print(f"[!] 请先执行 game_flow.py advance 完成所有步骤")
        return

    speeches = list(args.speech or [])
    vote_pairs = extract_votes_from_responses(args.vote or [], list(s["players"].keys()))
    hunter_target = None
    if args.hunter:
        skip = {"不", "没", "skip", "none", "空", "不开枪"}
        if not any(w in args.hunter for w in skip):
            t = extract_name(args.hunter, al)
            if t:
                hunter_target = t
    mechwolf_target = None
    if args.mechwolf and cfg.get("mechanical_wolf") and not cfg.get("mimic_wolf"):
        skip = {"不", "没", "skip", "none", "空", "不开枪"}
        if not any(w in args.mechwolf for w in skip):
            t = extract_name(args.mechwolf, al)
            if t:
                mechwolf_target = t
    white_wolf_targets = []
    if args.white_wolf and cfg.get("role_white_wolf_king"):
        for name in args.white_wolf:
            t = extract_name(name, al)
            if t:
                white_wolf_targets.append(t)
    vote_args = [f"{v}:{t}" for v, t in vote_pairs]
    import argparse
    ns = argparse.Namespace(
        speech=speeches, vote=vote_args,
        hunter_target=hunter_target,
        mechwolf_target=mechwolf_target,
        white_wolf_targets=white_wolf_targets,
        last_words=args.last_words,
        pass_sheriff=args.pass_sheriff,
        no_sheriff_confirm=args.no_sheriff_confirm,
        no_sheriff=getattr(args, 'no_sheriff', False),
        idiot_reveal=args.idiot_reveal,
        start_from=getattr(args, 'start_from', None),
    )
    cmd_day(ns)
