"""info.py — cmd_status, cmd_status_pretty, cmd_summary, cmd_stats, cmd_hint, cmd_journal"""

import json
from pathlib import Path

from state import load_game, save_game, log_event, record_game_result, LOG_FILE, STATS_FILE
from roles import (
    ROLES, ROLE_ICONS, DEFAULT_CONFIG, load_config,
    alive, wolves, find_player, is_wolf_role, pick_roles, is_final_battle,
)
from utils import extract_name


def cmd_status(args):
    s = load_game()
    al = alive(s)
    print(f"回合: {s['round']} | {'已结束' if s.get('winner') else s['phase']}")
    if s.get("winner"):
        print(f"胜利: {'好人' if s['winner']=='good' else '狼人' if s['winner']=='evil' else '平局'}")
    print(f"存活: {len(al)} 人")
    for n in sorted(s["players"], key=lambda x: s["players"][x]["role"]):
        p = s["players"][n]
        st = "[O]" if p["alive"] else "[X]"
        extra = ""
        if p.get("idiot_revealed"):
            extra = " (白痴·翻牌)"
        if p.get("mimic_target"):
            extra += f" →模仿{ROLES.get(p['mimic_target'], p['mimic_target'])}"
        print(f"  {st} {n} -> {ROLES[p['role']]}{extra}")


def cmd_status_pretty(args):
    s = load_game()
    al = alive(s)
    wc = len(wolves(s))
    phase = "已结束" if s.get("winner") else s["phase"]
    phase_icon = {'night':'🌙 夜晚','day':'🌅 白天','vote':'🗳️ 投票'}
    print(f"╔══ 第 {s['round']} {'回合' if phase=='已结束' else '回合 — ' + phase_icon.get(phase, phase)} ")
    alive_list = ", ".join(al)
    dead_list = ", ".join(n for n, p in s["players"].items() if not p["alive"])
    print(f"║ 存活 ({len(al)}): {alive_list}")
    if dead_list:
        print(f"║ 死亡: {dead_list}")
    events = [e for e in s.get("log", []) if "昨夜" in e or "狼人刀了" in e or "女巫救" in e or "女巫毒" in e or "预言家查验" in e or "猎人" in e or "处决" in e or "平安" in e]
    if events:
        last_events = events[-3:]
        print(f"║ 📋 事件: {' | '.join(e[:40] for e in last_events)}")
    if args.roles:
        print(f"╠══ 角色详情")
        for n in sorted(s["players"], key=lambda x: s["players"][x]["role"]):
            p = s["players"][n]
            st = "✅" if p["alive"] else "💀"
            extra = ""
            if s.get("sheriff") == n:
                extra += " 👑警长"
            if p.get("idiot_revealed"):
                extra += " (翻牌)"
            if p.get("mimic_target"):
                extra += f" →{ROLES.get(p['mimic_target'], p['mimic_target'])}"
            print(f"║ {st} {ROLE_ICONS[p['role']]} {n} = {ROLES[p['role']]}{extra}")
    else:
        print(f"╠══ {wc} 🐺 狼人存活")
    if s.get("extra_night_kill"):
        print(f"║ ⚡ 双刀待触发")
    if s.get("winner"):
        print(f"╚══ 🏆 {'好人' if s['winner']=='good' else '狼人' if s['winner']=='evil' else '平局'}阵营获胜！")
    else:
        print(f"╚══")


def cmd_summary(args):
    s = load_game()
    cfg = load_config()
    al = alive(s)
    rn = s["round"]
    nl = f"第{rn-1}晚" if s["phase"]=="night" and rn>1 else "第0晚" if s["phase"]=="night" else f"第{rn}天"
    pf = args.for_player
    prole = s["players"][pf]["role"] if pf and pf in s["players"] else None
    is_w = prole in ("wolf","mech_wolf") if prole else False

    print(f"═══ {nl} ══ 存活{len(al)}人")
    if pf:
        print(f"├ 你: {ROLE_ICONS.get(prole,"")}{pf}={ROLES.get(prole,prole)}")
        if is_w:
            mates = [n for n in al if n!=pf and s["players"][n]["role"] in ("wolf","mech_wolf")]
            if mates: print(f"├ 狼队友: {" ".join(mates)}")
    else:
        gods = [n for n in al if s["players"][n]["role"] in ("seer","witch","hunter")]
        if gods: print(f"├ 神职: {" ".join(ROLE_ICONS[s["players"][n]["role"]]+n for n in gods)}")
    al_str = " ".join(al)
    print(f"├ 存活: {al_str}")
    dead = [n for n,p in s["players"].items() if not p["alive"]]
    if dead: print(f"├ 死亡: {" ".join(dead)}")
    if s.get("sheriff"): print(f"├ 👑警长: {s["sheriff"]}")
    print(f"├ 投票规则：警长 1.5 票，其余 1 票")
    if args.with_history:
        entries = s.get("log", [])
        if entries:
            print(f"├ 事件:")
            for e in entries[-10:]:
                print(f"│ {e[:120]}")
        speeches_shown = 0
        for n, p in sorted(s["players"].items()):
            for sp in p.get("speeches", []):
                speeches_shown += 1
                if speeches_shown <= 15:
                    print(f"│ 💬 {n}: {sp}")
        votes_shown = 0
        for n, p in sorted(s["players"].items()):
            for v in p.get("votes", []):
                votes_shown += 1
                if votes_shown <= 10:
                    print(f"│ 🗳 {n}→{v}")
    print(f"═══")


def cmd_stats(args):
    """显示对局统计。支持 --pretty 详细面板。"""
    if not Path(STATS_FILE).exists():
        _empty_stats(); return
    try:
        records = json.loads(Path(STATS_FILE).read_text(encoding="utf-8"))
    except Exception:
        print("[!] 统计文件损坏"); return
    total = len(records)
    if total == 0:
        _empty_stats(); return
    if args.pretty:
        _show_pretty_stats(records)
    else:
        _show_basic_stats(records)


def _empty_stats():
    print("暂无对局记录")


def _show_basic_stats(records):
    total = len(records)
    good = sum(1 for r in records if r.get("winner") == "good")
    evil = sum(1 for r in records if r.get("winner") == "evil")
    draw = total - good - evil
    print("=" * 36)
    print(f"  总对局 : {total}")
    print(f"  好人胜 : {good} ({good*100//total}%)")
    print(f"  狼人胜 : {evil} ({evil*100//total}%)")
    if draw:
        print(f"  平局   : {draw}")
    print(f"  平均轮次: {sum(r.get('rounds',0) for r in records)//total}")
    print("=" * 36)


def _show_pretty_stats(records):
    total = len(records)
    good = sum(1 for r in records if r.get("winner") == "good")
    evil = sum(1 for r in records if r.get("winner") == "evil")
    draw = total - good - evil

    # 角色登场次数 & 阵营胜率
    role_stats = {}  # role -> {"count": n, "wins": n}
    avg_rounds_by_winner = {"good": [], "evil": [], "draw": []}
    player_ranks = {}  # player -> {"count": n, "good_wins": n, "evil_wins": n}

    for r in records:
        w = r.get("winner", "draw")
        avg_rounds_by_winner.setdefault(w, []).append(r.get("rounds", 0))
        roles = r.get("roles", {})
        survivors = set(r.get("survivors", []))
        for player, role in roles.items():
            if role not in role_stats:
                role_stats[role] = {"count": 0, "wins": 0, "total": 0}
            role_stats[role]["count"] += 1
            role_stats[role]["total"] += 1
            if w == "good" and player in survivors:
                role_stats[role]["wins"] += 1
            if w == "evil" and player not in survivors:
                role_stats[role]["wins"] += 1
            elif w == "evil":
                role_stats[role]["wins"] += 1

    # 标题
    print()
    print("=" * 50)
    print("  🎲  狼人杀对局统计面板")
    print("=" * 50)

    # 基本统计
    print(f"\n  📊  总对局: {total}")
    print(f"  {'好人胜':>8}: {good:>3} ({good*100//total:>2}%)  "
          f"{'狼人胜':>8}: {evil:>3} ({evil*100//total:>2}%)"
          + (f"  平局: {draw}" if draw else ""))
    all_rounds = [r.get("rounds", 0) for r in records]
    print(f"  平均轮次: {sum(all_rounds)//total}  "
          f"(最快: {min(all_rounds)} 最慢: {max(all_rounds)})")

    # 角色胜率
    if role_stats:
        print(f"\n  {'─'*46}")
        print(f"  {'角色胜率':^44}")
        print(f"  {'─'*46}")
        print(f"  {'角色':<12} {'登场':>6} {'胜局':>6} {'胜率':>7}")
        print(f"  {'─'*46}")
        for role in sorted(role_stats, key=lambda r: -role_stats[r]["count"]):
            rs = role_stats[role]
            name = ROLES.get(role, role)
            win_rate = rs["wins"] * 100 // max(rs["total"], 1)
            bar = "█" * (win_rate // 10) + "░" * (10 - win_rate // 10)
            print(f"  {name:<12} {rs['count']:>6} {rs['wins']:>6}  {win_rate:>3}% {bar}")
        print(f"  {'─'*46}")

    print()
    print("=" * 50)


def cmd_hint(args):
    """输出当前角色在当前局势下的策略提示（含决胜局战略指导）。"""
    s = load_game()
    cfg = load_config()
    al = alive(s)
    role = args.role
    wc = len(wolves(s, cfg))
    gc = len(al) - wc
    rn = s["round"]
    phase = s["phase"]

    is_final, final_level = is_final_battle(s, cfg)

    alive_roles = {}
    for n in al:
        r = s["players"][n]["role"]
        alive_roles[r] = alive_roles.get(r, 0) + 1

    witch_antidote_used = any(p.get("witch_antidote_used") for p in s["players"].values())
    witch_poison_used = any(p.get("witch_poison_used") for p in s["players"].values())

    guard_last = s.get("last_guard_target", "无")

    seer_checks = []
    for n, p in s["players"].items():
        if p["role"] == "seer" and p["alive"]:
            seer_checks = [log for log in s.get("log", []) if "查验" in log or "查杀" in log or "金水" in log]

    print(f"[HINT] 角色={ROLES.get(role,role)} 存活{len(al)}人 狼{wc}狼 好人{gc}人 轮次={rn} 阶段={phase}")

    if is_final:
        print(f"[HINT] ⚠️ 决胜局！狼{wc}狼 vs 好人{gc}人")
        if wc >= gc:
            print(f"[HINT] 🔴 狼人数量 ≥ 好人数量，好人投错就输！")
        elif wc * 2 >= gc:
            print(f"[HINT] 🟡 狼人数量接近好人，局势紧张！")

    print(f"[HINT] 存活角色: {' '.join(f'{ROLES.get(r,r)}×{c}' for r,c in alive_roles.items())}")
    print(f"[HINT] 药水: 解药{'已用' if witch_antidote_used else '可用'} 毒药{'已用' if witch_poison_used else '可用'}")
    print(f"[HINT] 守卫上晚守了: {guard_last}")

    if seer_checks:
        print(f"[HINT] 预言家查验记录:")
        for check in seer_checks[-3:]:
            print(f"[HINT]   {check}")

    if role == "wolf":
        gods_alive = [n for n in al if s["players"][n]["role"] in ("seer","witch","hunter","guard")]
        if gods_alive:
            print(f"[HINT] 存活神职: {', '.join(gods_alive)}")
        if is_final:
            print(f"[HINT] 【决胜局策略】")
            print(f"[HINT] - 刀人优先级: 预言家 > 女巫 > 守卫 > 猎人 > 村民")
            print(f"[HINT] - 活着的神职越少，狼人优势越大")
            print(f"[HINT] - 考虑谁是最后一个神，优先刀掉")
            print(f"[HINT] - 千层饼博弈：")
            print(f"[HINT]   L0: 刀最大威胁（预言家）")
            print(f"[HINT]   L1: 守卫守预 → 刀女巫")
            print(f"[HINT]   L2: 守卫守女巫 → 回头刀预言家")
            print(f"[HINT]   L3: 混合策略最优")
        else:
            print(f"[HINT] 【常规策略】")
            print(f"[HINT] - 和队友统一刀人目标")
            print(f"[HINT] - 首夜盲刀，不要猜身份")

    elif role == "seer":
        if is_final:
            print(f"[HINT] 【决胜局策略】")
            print(f"[HINT] - 预言家是狼人首要刀目标，注意隐藏")
            print(f"[HINT] - 尽快验出关键信息，带队归票")
            print(f"[HINT] - 如果被查杀，可以要求守卫守你")
            print(f"[HINT] - 千层饼博弈：")
            print(f"[HINT]   L0: 狼人刀你 → 守卫守你")
            print(f"[HINT]   L1: 狼人转刀女巫 → 守卫守女巫")
            print(f"[HINT]   L2: 狼人回头刀你 → 需要守卫反向思维")
        else:
            print(f"[HINT] 【常规策略】")
            print(f"[HINT] - 查验划水牌或焦点牌")
            print(f"[HINT] - 隐藏型不跳，带队型可跳")

    elif role == "witch":
        print(f"[HINT] 解药{'已用' if witch_antidote_used else '可用'} 毒药{'已用' if witch_poison_used else '可用'}")
        if is_final:
            print(f"[HINT] 【决胜局策略】")
            print(f"[HINT] - 女巫是狼人次要刀目标，注意隐藏")
            print(f"[HINT] - 解药：救人优先级 预言家 > 守卫 > 猎人")
            print(f"[HINT] - 毒药：关键轮次前毒，打乱狼队节奏")
            print(f"[HINT] - 如果被刀，考虑是否自救")
            print(f"[HINT] - 千层饼博弈：")
            print(f"[HINT]   L0: 救被刀的人 + 毒一个嫌疑狼")
            print(f"[HINT]   L1: 不救 + 毒一个（可能只剩3人）")
            print(f"[HINT]   L2: 狼人故意刀队友做深水狼？")
        else:
            print(f"[HINT] 【常规策略】")
            print(f"[HINT] - 首夜救人性价比最高")
            print(f"[HINT] - 毒药：必须满足3条件中2条才毒")

    elif role == "hunter":
        if is_final:
            print(f"[HINT] 【决胜局策略】")
            print(f"[HINT] - 猎人是狼人考虑的刀目标（怕被开枪带走）")
            print(f"[HINT] - 被投死/被刀死时开枪带走疑似狼")
            print(f"[HINT] - 不确定时选择不开枪，保留威慑")
            print(f"[HINT] - 如果是最后一神，隐藏身份")
            print(f"[HINT] - 千层饼博弈：")
            print(f"[HINT]   L0: 被投死 → 开枪带走最大威胁")
            print(f"[HINT]   L1: 狼人知道你会开枪 → 可能不投你")
            print(f"[HINT]   L2: 利用威慑不被投票，隐藏到最后")
        else:
            print(f"[HINT] 【常规策略】")
            print(f"[HINT] - 被刀可开枪(非毒杀)")
            print(f"[HINT] - 不确定时可不开枪")

    elif role == "guard":
        if is_final:
            print(f"[HINT] 【决胜局策略】")
            print(f"[HINT] - 守卫是狼人最后才刀的目标（守卫没有主动技能）")
            print(f"[HINT] - 守人优先级: 预言家 > 女巫 > 猎人")
            print(f"[HINT] - 与女巫配合，避免同守同救")
            print(f"[HINT] - 博弈：狼人会分析你守谁，考虑反向思维")
            print(f"[HINT] - 千层饼博弈：")
            print(f"[HINT]   L0: 守最大威胁（预言家）")
            print(f"[HINT]   L1: 狼人转刀女巫 → 守女巫")
            print(f"[HINT]   L2: 狼人回头刀预言家 → 守预言家")
            print(f"[HINT]   L3: 混合策略最优（随机守/空过）")
        else:
            print(f"[HINT] 【常规策略】")
            print(f"[HINT] - 首夜推荐空过，获得信息优势")
            print(f"[HINT] - 不能连续两晚守同一人")

    elif role == "villager":
        if is_final:
            print(f"[HINT] 【决胜局策略】")
            print(f"[HINT] - 村民是狼人最想刀的目标（无技能）")
            print(f"[HINT] - 跟票站边，不要暴露")
            print(f"[HINT] - 如果被投死，没有技能，尽量提供信息")
        else:
            print(f"[HINT] 【常规策略】")
            print(f"[HINT] - 跟票站边，不要暴露")
            print(f"[HINT] - 发言简洁，隐藏身份")

    elif role == "idiot":
        if is_final:
            print(f"[HINT] 【决胜局策略】")
            print(f"[HINT] - 白痴翻牌后不死但失去投票权，每票都关键")
            print(f"[HINT] - 被投时选择是否翻牌：翻牌保命但少一票")
            print(f"[HINT] - 如果好人票数紧张，宁死不翻保投票权")
        else:
            print(f"[HINT] 【常规策略】")
            print(f"[HINT] - 被投时可翻牌不死，但失去投票权")
            print(f"[HINT] - 翻牌后仍可发言，继续提供信息")

    elif role == "mech_wolf":
        if is_final:
            print(f"[HINT] 【决胜局策略】")
            print(f"[HINT] - 机械狼被投可击杀一人，是重要威慑")
            print(f"[HINT] - 模仿模式下：预言家查验显示模仿身份")
            print(f"[HINT] - 考虑是否需要暴露身份换掉关键好人")
        else:
            print(f"[HINT] 【常规策略】")
            print(f"[HINT] - 被投时可击杀一人（非模仿模式）")
            print(f"[HINT] - 模仿模式：第1晚选择模仿身份")

    if is_final:
        print(f"[HINT] 【通用决胜局原则】")
        print(f"[HINT] - 每一票都决定胜负，谨慎投票")
        print(f"[HINT] - 狼人数量 = 好人数量时，狼人刀对就赢")
        print(f"[HINT] - 好人投对就赢，但投错就输")
        print(f"[HINT] - 考虑对手的思考层级（千层饼）")


def cmd_journal(args):
    """输出本局可读战报。"""
    s = load_game()
    cfg = load_config()
    print(f"╔══ 狼人杀战报 ══")
    print(f"║ 玩家{len(s['players'])}人 | 规则: witch_self_save_n1={cfg.get('witch_self_save_n1')}")
    wc = len([p for p in s['players'].values() if p['role']=='wolf'])
    print(f"║ {wc}狼局")
    if s.get("winner"):
        print(f"║ 🏆 {s['winner']}")
    print(f"║ 回合: {s['round']}")
    print(f"╠══ 角色 ══")
    for n, p in sorted(s['players'].items(), key=lambda x: x[1]['role']):
        st = "✅" if p['alive'] else "💀"
        print(f"║ {st} {ROLE_ICONS[p['role']]} {n} = {ROLES[p['role']]}")
    print(f"╠══ 事件时间线 ══")
    if Path(LOG_FILE).exists():
        with open(LOG_FILE, encoding='utf-8') as f:
            for line in f:
                try:
                    e = json.loads(line)
                    if e.get('type') == 'night_death':
                        d = e['data']['dead']
                        r = e['data']['round']
                        print(f"║ 第{r}晚死亡: {', '.join(d)}")
                except:
                    pass
    for log_entry in s.get("log", []):
        if "处决" in log_entry:
            print(f"║ {log_entry}")
    print(f"╚══")
