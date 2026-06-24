"""prompts.py — cmd_make_prompts"""

from state import load_game, save_game, log_event, record_game_result, LOG_FILE, STATS_FILE
from roles import (
    ROLES, ROLE_ICONS, DEFAULT_CONFIG, load_config,
    alive, wolves, find_player, is_wolf_role, pick_roles, is_final_battle,
)
from utils import extract_name
from prompt_builder import PromptBuilder


def cmd_make_prompts(args):
    """生成所有存活玩家的 prompt（GM 直接复制进 parallel_tasks）。"""
    s = load_game()
    cfg = load_config()
    al = alive(s)
    rn = s["round"]
    is_night = s["phase"] == "night"
    lbl = f"第{rn-1}晚" if is_night and rn > 1 else "第0晚" if is_night else f"第{rn}天"
    dead = [n for n, p in s["players"].items() if not p["alive"]]
    a = args.action
    with_history = True  # 默认始终携带历史上下文

    total_players = getattr(args, 'player_count', None) or len(s["players"])
    alive_count = len(al)

    if total_players <= 7:
        sheriff_ideal = 2
        wolf_sheriff_max = 1
        god_strategy = "hide"
        small_game = True
    elif total_players <= 9:
        sheriff_ideal = 3
        wolf_sheriff_max = 2
        god_strategy = "flexible"
        small_game = False
    else:
        sheriff_ideal = min(5, total_players // 2)
        wolf_sheriff_max = 2
        god_strategy = "standard"
        small_game = False

    history_events = s.get("log", [])[-10:] if with_history else []
    recent_speeches = []
    recent_votes = []
    if with_history:
        order = s.get("player_order", list(s["players"].keys()))
        for n in order:
            if n not in s["players"]:
                continue
            p = s["players"][n]
            for sp in p.get("speeches", [])[-2:]:
                recent_speeches.append(f"💬 {n}: {sp}")
            for v in p.get("votes", [])[-2:]:
                recent_votes.append(f"🗳 {n}→{v}")

    print(f"# {a} — {lbl} 存活{len(al)}人")

    for_player = getattr(args, 'for_player', None)
    if for_player:
        if for_player not in s["players"]:
            print(f"[!] 玩家 {for_player} 不存在")
            return
        if not s["players"][for_player]["alive"]:
            print(f"[!] 玩家 {for_player} 已死亡")
            return
        al = [for_player]

    for name in al:
        p = s["players"][name]
        r = p["role"]
        # 新式 action：委托给 PromptBuilder（含策略文件注入）
        if a in ("wolf_strategy", "wolf_adjust", "wolf_claim", "wolf_hunting", "wolf_deep", "good_day", "god_hide"):
            pb = PromptBuilder()
            prompt = pb.build(a, name)
            print(prompt)
            continue
        lines = [f"\n--- {name} ({ROLES.get(r,r)}) ---"]
        lines.append(f"存活: {' '.join(al)}")
        if dead: lines.append(f"死亡: {' '.join(dead)}")
        if r in ("wolf","mech_wolf"):
            mates = [n for n in al if n!=name and s["players"][n]["role"] in ("wolf","mech_wolf")]
            if mates: lines.append(f"狼队友: {' '.join(mates)}")
        if s.get("sheriff"): lines.append(f"警长: {s['sheriff']}")
        if with_history and history_events:
            lines.append("├ 事件:")
            for e in history_events:
                lines.append(f"│ {e[:120]}")
        if with_history and recent_speeches:
            lines.append("├ 发言:")
            for sp in recent_speeches[-5:]:
                lines.append(f"│ {sp[:200]}")
        if with_history and recent_votes:
            lines.append("├ 投票:")
            for v in recent_votes[-5:]:
                lines.append(f"│ {v}")
        last = [e for e in s.get("log",[]) if "昨夜" in e or "处决" in e]
        if last and not with_history: lines.append(last[-1])
        if a == "speech":
            lines.append(f"请发言，≤200字。")
            if small_game:
                lines.append("【小局策略】发言简洁，隐藏身份，避免暴露神职。")
            if r == "wolf":
                lines.append("【狼人策略】可冲锋/倒钩/深水，根据局势选择。")
            elif r in ("seer", "witch", "hunter", "guard"):
                if god_strategy == "hide":
                    lines.append("【神职策略】建议隐藏，跟票站边，非必要不跳。")
                elif god_strategy == "flexible":
                    lines.append("【神职策略】可灵活选择隐藏或带队。")
        elif a == "vote":
            lines.append("投谁？只需名字。")
            lines.append("规则：")
            lines.append("- 必须给出一个质疑点")
            lines.append("- 不能说'觉得XX像好人就投了'")
            lines.append("- 输出格式：直接输出一个名字")
            lines.append(f"- 只能投存活玩家之一：{'、'.join(alive(s))}")
            if r == "wolf":
                lines.append("- 你是狼人，可以冲票或倒钩")
                if wolf_sheriff_max >= 2 and alive_count >= 8:
                    lines.append("- 大局可考虑倒钩做身份")
            if with_history and recent_speeches:
                lines.append("发言记录：")
                for sp in recent_speeches[-5:]:
                    lines.append(f"  {sp[:200]}")
        elif a == "night_kill" and r in ("wolf","mech_wolf"):
            if rn == 1:
                lines.append("首夜盲刀：刀谁？理由？（≤50字）")
                lines.append("【首夜战术】（必须回答）")
                lines.append("1. 谁上警悍跳预言家？查验目标是谁？")
                lines.append("2. 谁倒钩深水（不上警，站边真预）？")
                lines.append("3. 谁冲锋（上警但不跳预）？")
                lines.append("4. 刀人策略：屠神（优先刀预言家/女巫）还是屠民？")
                lines.append("格式：刀:【名字】 悍跳:【名字】 倒钩:【名字】 冲锋:【名字】")
            else:
                lines.append("刀谁？理由？（≤50字）")
                lines.append("【策略检查】")
                lines.append("- 你当前的角色（冲锋/倒钩/深水）是否需要调整？")
                lines.append("- 谁最可能是神职？")
                lines.append("- 刀谁对狼队最有利？")
                if with_history and recent_speeches:
                    lines.append("【最近发言】")
                    for sp in recent_speeches[-3:]:
                        lines.append(f"  {sp[:80]}")
        elif a == "wolf_adjust" and r in ("wolf","mech_wolf"):
            lines.append("根据白天发言，调整你的策略：")
            lines.append("1. 你当前的角色（冲锋/倒钩/深水）是否需要调整？")
            lines.append("2. 谁最可能是神职？")
            lines.append("3. 你明天的发言策略？")
            if with_history and recent_speeches:
                lines.append("【发言记录】")
                for sp in recent_speeches[-5:]:
                    lines.append(f"  {sp[:200]}")
        elif a == "night_check" and r=="seer":
            lines.append("查验谁？理由？（≤50字）")
            if rn == 1:
                lines.append("【首夜查验策略】")
                lines.append("- 查验优先级：疑似狼面大的人 > 中置位划水牌 > 焦点位")
                lines.append("- 不验边角位（位置学无意义）")
                lines.append("- 验疑似狼：直接拍查杀，压缩狼队空间")
                lines.append("- 验划水：划水可能是深水狼，验出身份帮助判断")
            else:
                lines.append("【后续查验策略】")
                lines.append("- 查验优先级：未验过的疑似狼 > 站边摇摆的人 > 被多人保的人")
                lines.append("- 不重复验同一个人")
                lines.append("- 验出查杀：直接拍，考虑是否留一轮观察")
                lines.append("- 验出金水：用来带队、归票、组狼坑")
            seer_logs = [log for log in s.get("log", []) if "查验" in log or "查杀" in log or "金水" in log]
            if seer_logs:
                lines.append("【已查验】")
                for log in seer_logs[-2:]:
                    lines.append(f"  {log[:80]}")
        elif a == "night_guard" and r=="guard":
            lines.append("守谁或空过？理由？（≤50字）")
            if rn == 1:
                lines.append("【首夜策略】")
                lines.append("- 推荐空过：获得信息优势，避免同守同救风险")
                lines.append("- 守人优先级：预言家 > 女巫 > 银水 > 空过")
                lines.append("- 博弈层次：L1你守预言家→狼刀女巫；L2你守女巫→狼刀预言家")
            else:
                lines.append("【后续策略】")
                lines.append("- 连守限制：不能连续两晚守同一人")
                lines.append("- 守人优先级：预言家 > 女巫 > 银水 > 焦点位")
                lines.append("- 与女巫配合：女巫已跳→注意同守同救风险")
                lines.append("- 平安夜信息利用：反推狼人刀法")
            guard_target = s.get("last_guard_target", None)
            if guard_target:
                lines.append(f"【上一晚守了】{guard_target}")
        elif a == "witch" and r == "witch":
            witch_name = None
            for n, p in s["players"].items():
                if p["role"] == "witch" and p["alive"]:
                    witch_name = n
                    break
            if witch_name:
                witch_antidote = not s["players"][witch_name].get("witch_antidote_used", False)
                witch_poison = not s["players"][witch_name].get("witch_poison_used", False)
                lines.append(f"药水状态：解药{'可用' if witch_antidote else '已用'} 毒药{'可用' if witch_poison else '已用'}")
            lines.append("救/毒/跳过？给出决定+理由。")
            last_kill = None
            # 优先使用 --kill 参数（GM在狼队讨论后传入）
            kill_override = getattr(args, 'kill', None)
            if kill_override:
                last_kill = kill_override
            else:
                for log in reversed(s.get("log", [])):
                    if "狼人刀了" in log:
                        last_kill = log.replace("狼人刀了 ", "")
                        break
            if last_kill:
                lines.append(f"【今晚被刀】{last_kill}")
            lines.append("【解药决策】")
            lines.append("- 首夜救人性价比最高（信息最多）")
            lines.append("- 银水大概率是好人（但可能是自刀狼）")
            lines.append("- 如果被杀的是疑似预言家/猎人 → 救")
            lines.append("【毒药决策框架】（必须同时满足2条以上才毒）")
            lines.append("1. 身份明确：已被预言家查杀为狼，或被多轮逻辑锤死")
            lines.append("2. 无反转可能：不是'站错边的好人'或'逻辑混乱的村民'")
            lines.append("3. 威胁评估：是狼队核心，不毒会继续带节奏")
            lines.append("【毒药禁忌】")
            lines.append("- 不要因为'某人让我毒他'就毒 → 狼人可能在骗毒")
            lines.append("- 不要因为'某人发言不好'就毒 → 发言差≠狼")
            lines.append("- 宁可不毒，也不要毒错好人（毒错=送轮次）")
        elif a == "sheriff":
            lines.append(f"第1天警上竞选环节（{alive_count}人局，建议{sheriff_ideal}人上警）。")
            lines.append("你是否要上警？只需说'上警'或'不上'。")
            if r == "seer":
                lines.append("你是真正的预言家！如果上警，报查验+警徽流。")
            elif r == "wolf":
                lines.append(f"你是狼人！建议{wolf_sheriff_max}狼上警悍跳，其余倒钩深水。")
                lines.append("策略选项：")
                lines.append("- 悍跳预言家（编造查验），需队友配合")
                lines.append("- 倒钩：不上警，站边真预，做身份")
                lines.append("- 冲锋：上警但不跳预，投票时冲票")
            elif r in ("witch", "hunter", "guard"):
                if god_strategy == "hide":
                    lines.append(f"你是{ROLES.get(r, r)}！小局建议隐藏，非必要不跳身份。")
                elif god_strategy == "flexible":
                    lines.append(f"你是{ROLES.get(r, r)}！可选择隐藏或穿衣服挡刀。")
                else:
                    lines.append(f"你是{ROLES.get(r, r)}！可根据局势灵活选择。")
                lines.append("- 不要无理由宣称预言家，会暴露身份")
            elif r == "idiot":
                lines.append(f"你是白痴！建议隐藏，被投时可翻牌。")
            else:
                lines.append(f"你是村民！建议隐藏，跟票站边即可。")
            lines.append("输出格式：'上警'或'不上'。")
        elif a == "sheriff_withdraw":
            lines.append("警上退水环节。")
            # 找场上对跳信息
            claimed_seers = []
            for log_entry in s.get("log", []):
                if "跳预言家" in log_entry or "跳预" in log_entry:
                    name = log_entry.split("跳预")[0].strip()
                    if name not in claimed_seers:
                        claimed_seers.append(name)
            if claimed_seers:
                lines.append(f"场上跳预言家的玩家：{'、'.join(claimed_seers)}")
            lines.append("")
            lines.append("退水规则：")
            lines.append("- 退水 → 恢复投票权，可在警下投票")
            lines.append("- 不退水 → 继续竞选，不能投票")
            lines.append("")
            # 角色专属策略
            if r == "wolf":
                if "悍跳" in str(args):
                    lines.append("你是悍跳预言家！绝对不能退水。")
                    lines.append("退水=认狼，狼队崩盘。必须拿警徽出真预。")
                else:
                    lines.append("你是狼人！建议退水恢复投票权。")
                    lines.append("退水后可在警下投票，为队友冲票或倒钩做身份。")
            elif r == "seer":
                lines.append("你是真预言家！绝对不能退水。")
                lines.append("退水=把警徽让给悍跳狼。必须拿警徽带队归票、留警徽流。")
            elif r == "guard":
                lines.append("你是守卫！强烈建议退水。")
                lines.append("守卫拿警徽=吃首刀，退水藏身份才是正确打法。")
                lines.append("退水后可投票给真预言家。")
            elif r == "witch":
                lines.append("你是女巫！建议退水。")
                lines.append("女巫是强神，拿警徽容易成为狼人刀口。")
                lines.append("退水藏身份，用毒药带队更有效。")
            elif r == "hunter":
                lines.append("你是猎人！可退水可不退：")
                lines.append("- 退水：恢复投票权，藏身份，被投死可开枪")
                lines.append("- 不退水：拿警徽带队，但吃刀风险高")
            elif r == "idiot":
                lines.append("你是白痴！建议退水。")
                lines.append("白痴翻牌后只剩发言，拿警徽意义不大。")
            else:
                lines.append("你是村民！建议退水。")
                lines.append("村民没技能，退水让位给预言家/神职更合理。")
            lines.append("")
            lines.append("输出格式：退/不退 + 一句话理由（≤30字）")
        elif a == "last_words":
            lines.append("你已死亡，请发表遗言（≤150字）。")
            lines.append("可以：报身份、点狼、给信息、表明立场。")
        elif a == "hunter_active":
            if r == "hunter":
                if cfg.get("hunter_active_shot"):
                    lines.append("你是猎人！白天可主动亮身份开枪。")
                    lines.append("是否开枪？目标是谁？理由？")
                    lines.append("注意：开枪后你自己也会死亡。")
                else:
                    lines.append("你是猎人！当前规则不允许主动开枪。")
                    lines.append("你只能在被处决/被狼刀时被动开枪。")
            else:
                lines.append("你不是猎人！无法执行此操作。")
        elif a == "idiot_reveal":
            if r == "idiot":
                lines.append("你是白痴！被投票处决时可选择翻牌。")
                lines.append("翻牌后：不死，但失去投票权，可继续发言。")
                lines.append("是否翻牌？只需说'翻牌'或'不翻'。")
            else:
                lines.append("你不是白痴！无法执行此操作。")
        elif a == "wolf_explode":
            if r in ("wolf", "mech_wolf"):
                lines.append("你是狼人！可选择自爆。")
                lines.append("自爆后：跳过当天投票，直接进入黑夜。")
                lines.append("是否自爆？只需说'自爆'或'不自爆'。")
            else:
                lines.append("你不是狼人！无法执行此操作。")
        elif a == "mimic":
            if r == "mech_wolf":
                lines.append("你是机械狼！第1晚选择模仿身份。")
                lines.append("模仿后可使用该身份的技能。")
                lines.append("选择模仿谁？只说角色名（seer/witch/hunter/guard/villager/wolf）。")
            else:
                lines.append("你不是机械狼！无法执行此操作。")
        else: lines.append("请按角色行动。")

        depth = getattr(args, 'depth', 'basic')
        if depth in ("analysis", "game-theory"):
            lines.append("")
            lines.append("【分析层】")
            seer_logs = [log for log in s.get("log", []) if "查验" in log]
            if seer_logs:
                lines.append(f"已知查验: {'; '.join(seer_logs[-3:])}")
            witch_a = any(p.get("witch_antidote_used") for p in s["players"].values())
            witch_p = any(p.get("witch_poison_used") for p in s["players"].values())
            lines.append(f"药水: 解药{'已用' if witch_a else '可用'} 毒药{'已用' if witch_p else '可用'}")
            guard_t = s.get("last_guard_target")
            if guard_t:
                lines.append(f"上晚守了: {guard_t}")
            vote_logs = [log for log in s.get("log", []) if "处决" in log]
            if vote_logs:
                lines.append(f"历史处决: {'; '.join(vote_logs[-3:])}")

        if depth == "game-theory":
            is_final, fl = is_final_battle(s, cfg)
            if is_final:
                lines.append("")
                lines.append("【博弈层 - 决胜局】")
                all_alive = [n for n, p in s["players"].items() if p["alive"]]
                wc = len([n for n in all_alive if s["players"][n]["role"] in ("wolf","mech_wolf")])
                gc = len(all_alive) - wc
                lines.append(f"狼{wc} vs 好人{gc}")
                if r in ("wolf", "mech_wolf"):
                    lines.append("千层饼分析:")
                    lines.append("L0: 你刀最大威胁（预言家）")
                    lines.append("L1: 守卫会守预言家 → 你刀女巫")
                    lines.append("L2: 守卫预判你刀女巫 → 守女巫 → 你回头刀预言家")
                    lines.append("L3: 混合策略最优（随机刀A/B）")
                elif r == "guard":
                    lines.append("千层饼分析:")
                    lines.append("L0: 狼人刀预言家 → 你守预言家")
                    lines.append("L1: 狼人预判你守预 → 刀女巫 → 你守女巫")
                    lines.append("L2: 狼人预判你守女巫 → 回头刀预言家")
                    lines.append("L3: 混合策略最优（随机守A/B/空过）")
                else:
                    lines.append("每票决定胜负，谨慎投票")
                    lines.append("分析谁发言最像狼，谁投票最可疑")

        print("\n".join(lines))
