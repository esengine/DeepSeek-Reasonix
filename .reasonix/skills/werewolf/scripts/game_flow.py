#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""game_flow.py — 游戏流程控制器，提示GM下一步操作"""

import sys, json, argparse
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
from state import load_game, save_game
from roles import alive, ROLES, load_config

FLOW_KEY = "_flow"

NIGHT_FLOW = [
    ("guard", "守卫守护", "task问守卫守谁或空过（不能连守同一人）"),
    ("wolf_proposal", "狼队独立提案", "parallel_tasks问每狼独立提案（刀谁+想悍跳/倒钩/冲锋）"),
    ("wolf_discuss", "狼队集体讨论", "parallel_tasks让所有狼看到彼此提案，根据当前版面决定大致战术，讨论分歧"),
    ("wolf_vote", "狼队投票决策", "parallel_tasks投票决定最终方案（有分歧时）"),
    ("wolf_assign", "狼队分工确认", "task确认最终分工（谁悍跳+谁倒钩+刀谁+查杀目标）"),
    ("witch_save", "女巫救人", "task问女巫是否救被刀的人（有解药才问，救了跳过毒人阶段）"),
    ("witch_poison", "女巫毒人", "task问女巫是否毒人（救人没成功/解药用过才问）"),
    ("seer", "预言家查验", "task问预言家查验谁（被刀仍可查验）"),
    ("death", "死亡判定", "自动：计算狼刀+女巫救/毒+守卫守护结果"),
    ("hunter", "猎人被刀开枪", "task问被刀死的猎人是否开枪（被毒死不能开）"),
    ("last_words", "夜间遗言", "task问死者遗言（首夜或night_kill_last_words=true时）"),
    ("sheriff", "警长传位", "task问死去的警长传给谁（不加参数则警徽流失）"),
    ("extra_kill", "双刀（模仿狼）", "self-auto: 狼自爆后机械狼获得额外刀人机会"),
]

DAY_FLOW = [
    ("explode", "狼自爆检查", "并行task问每只狼是否自爆（仅wolf_explode=true时）"),
    ("sheriff", "警长竞选", "第1天且无警长时：parallel_tasks上警意愿→候选人发言→sheriff命令"),
    ("direction", "发言方向", "task问警长从左还是右开始发言（sheriff-direction 左|右）"),
    ("speech", "发言", "parallel_tasks问所有人发言"),
    ("vote", "投票", "parallel_tasks问所有人投票"),
    ("idiot_check", "白痴翻牌", "task问被处决的白痴是否翻牌（被投死才问）"),
    ("execute", "处决", "执行投票结果（含猎人开枪/狼王开枪/警长传位）"),
    ("last_words", "遗言", "task问被处决者/被带走者遗言（≤150字）"),
]

VOTE_FLOW = [
    ("vote", "投票", "parallel_tasks问所有人投票"),
    ("execute", "处决", "自动执行投票结果"),
]


def ensure_flow(s):
    if FLOW_KEY not in s:
        s[FLOW_KEY] = {"night_step": 0, "day_step": 0, "completed_steps": []}
    f = s[FLOW_KEY]
    if f.get("_phase") != s.get("phase") or f.get("_round") != s.get("round", 1):
        f["_phase"] = s.get("phase")
        f["_round"] = s.get("round", 1)
        f["completed_steps"] = []  # 重置完成状态
        if s.get("phase") == "night":
            f["night_step"] = 0
        elif s.get("phase") == "day":
            # 第1天从警长竞选开始；后续天跳过竞选
            if s.get("round", 1) >= 2:
                f["day_step"] = 2  # 跳过 explode(0) + sheriff(1)
            elif s.get("sheriff"):
                f["day_step"] = 2  # 已有警长，跳过竞选
            else:
                f["day_step"] = 0
        elif s.get("phase") == "vote":
            f["day_step"] = 0  # 投票阶段，从vote(0)开始
    return f


def mark_step_completed(s, action_id):
    """标记步骤为已完成"""
    f = ensure_flow(s)
    if "completed_steps" not in f:
        f["completed_steps"] = []
    if action_id not in f["completed_steps"]:
        f["completed_steps"].append(action_id)
    save_game(s)


def is_step_completed(s, action_id):
    """检查步骤是否已完成"""
    f = ensure_flow(s)
    return action_id in f.get("completed_steps", [])


def validate_night_steps(s):
    """验证夜间必需步骤是否已完成"""
    issues = []
    f = ensure_flow(s)
    completed = f.get("completed_steps", [])
    
    # 检查狼队讨论步骤
    wolf_steps = ["wolf_proposal", "wolf_discuss", "wolf_assign"]
    for step in wolf_steps:
        if step not in completed:
            issues.append(f"狼队讨论步骤未完成: {step}")
    
    # 检查女巫步骤
    witch = next((p for p in s["players"].values() if p["role"] == "witch" and p["alive"]), None)
    if witch:
        if not witch.get("witch_acted_tonight"):
            has_antidote = not witch.get("witch_antidote_used")
            has_poison = not witch.get("witch_poison_used")
            if has_antidote and "witch_save" not in completed:
                issues.append("女巫救人步骤未完成")
            elif has_poison and "witch_poison" not in completed:
                issues.append("女巫毒人步骤未完成")
    
    # 检查守卫步骤
    guard = next((p for p in s["players"].values() if p["role"] == "guard" and p["alive"]), None)
    if guard and "guard" not in completed:
        issues.append("守卫行动步骤未完成")
    
    # 检查预言家步骤
    seer = next((p for p in s["players"].values() if p["role"] == "seer" and p["alive"]), None)
    if seer and "seer" not in completed:
        issues.append("预言家查验步骤未完成")
    
    return issues


def validate_day_steps(s):
    """验证白天必需步骤是否已完成"""
    issues = []
    f = ensure_flow(s)
    completed = f.get("completed_steps", [])
    rnd = s.get("round", 1)
    
    # 检查发言步骤
    if "speech" not in completed:
        issues.append("发言步骤未完成")
    
    # 检查投票步骤
    if "vote" not in completed:
        issues.append("投票步骤未完成")
    
    # 第1天检查警长竞选
    if rnd == 1 and not s.get("sheriff") and "sheriff" not in completed:
        issues.append("警长竞选步骤未完成")
    
    # 检查发言方向（有警长时）
    if s.get("sheriff") and "direction" not in completed:
        issues.append("发言方向步骤未完成")
    
    return issues


def get_flow(s):
    phase = s.get("phase", "ended")
    flow_def = {"night": NIGHT_FLOW, "day": DAY_FLOW, "vote": VOTE_FLOW}.get(phase, [])
    f = ensure_flow(s)
    key = "night_step" if phase == "night" else "day_step"
    step = f.get(key, 0)
    return flow_def, step


def cmd_next(args):
    s = load_game()
    phase = s.get("phase", "ended")
    rnd = s.get("round", 1)
    cfg = load_config()
    flow_def, step = get_flow(s)

    if phase == "ended":
        winner = s.get("winner", "unknown")
        label = {"good": "好人阵营", "evil": "狼人阵营", "draw": "平局"}.get(winner, winner)
        print(f"游戏已结束，第 {rnd} 回合，{label} 获胜")
        save_game(s)
        return

    if step >= len(flow_def):
        phase_map = {"night": "night", "day": "day", "vote": "vote"}
        print(f"[第 {rnd} {'晚' if phase == 'night' else '天'}] 所有步骤已完成")
        print(f"[!] 请执行: {phase_map.get(phase, phase)} 命令提交结果并进入下一阶段")
        save_game(s)
        return

    action_id, name, hint = flow_def[step]
    total = len(flow_def)
    prefix = f"第 {rnd} {'晚' if phase == 'night' else '天'}"

    # 条件标注
    condition_tag = ""
    phase_hunter_dead = phase == "night" and any(
        p["role"] == "hunter" and p["alive"] == False
        for n, p in s["players"].items()
        if p.get("can_hunter_shoot") == True
    )
    if action_id == "hunter" and not phase_hunter_dead:
        condition_tag = " (跳过：猎人没死或不能开枪)"
    elif action_id == "last_words" and phase == "night" and not (rnd == 1 or cfg.get("night_kill_last_words")):
        condition_tag = " (跳过：首夜已过且night_kill_last_words=false)"
    elif action_id == "last_words" and phase == "day":
        condition_tag = ""  # 白天被处决必有遗言
    elif action_id == "extra_kill" and not cfg.get("double_kill_after_explode"):
        condition_tag = " (跳过：double_kill_after_explode=false)"
    elif action_id == "sheriff" and phase == "day" and (rnd >= 2 or s.get("sheriff")):
        condition_tag = " (跳过：已有警长或非第1天)"
    elif action_id == "direction" and not s.get("sheriff"):
        condition_tag = " (跳过：无警长)"
    elif action_id == "explode" and not cfg.get("wolf_explode"):
        condition_tag = " (跳过：wolf_explode=false)"
    elif action_id == "witch_save":
        witch = next((p for p in s["players"].values() if p["role"] == "witch" and p["alive"]), None)
        if not witch or witch.get("witch_antidote_used"):
            condition_tag = " (跳过：女巫没有解药)"
    elif action_id == "witch_poison":
        witch = next((p for p in s["players"].values() if p["role"] == "witch" and p["alive"]), None)
        if not witch:
            condition_tag = " (跳过：女巫已死)"
        elif witch.get("witch_poison_used"):
            condition_tag = " (跳过：女巫没有毒药)"
        elif witch.get("witch_acted_tonight") and witch.get("witch_saved_tonight"):
            condition_tag = " (跳过：女巫已救人成功)"
    elif action_id == "idiot_check":
        idiot = next((p for p in s["players"].values() if p["role"] == "idiot" and not p["alive"]), None)
        if not idiot or idiot.get("idiot_revealed"):
            condition_tag = " (跳过：无白痴被处决或已翻牌)"
    elif action_id == "wolf_vote":
        # 只有有分歧时才需要投票
        if not s.get("wolf_disagreement"):
            condition_tag = " (跳过：无分歧)"

    print(f"[{prefix}] 步骤 {step+1}/{total}: {name}{condition_tag}")
    print(f"  [{action_id}] {hint}")
    if step > 0:
        done = flow_def[:step]
        done_names = []
        for i, (did, dname, _) in enumerate(done):
            dn = dname
            # 对已完成步骤也加标注
            if did == "sheriff" and rnd >= 2:
                dn += "(跳过)"
            done_names.append(dn)
        print(f"  已完成: {' → '.join(done_names)}")
    remain = flow_def[step+1:]
    if remain:
        remain_names = [f[1] for f in remain]
        print(f"  剩余: {' → '.join(remain_names)}")
    save_game(s)


def cmd_status(args):
    s = load_game()
    phase = s.get("phase", "ended")
    rnd = s.get("round", 1)
    al = alive(s)
    flow_def, step = get_flow(s)

    phase_map = {"night": "夜晚", "day": "白天", "vote": "投票", "ended": "已结束"}
    print(f"回合: 第 {rnd} 轮")
    print(f"阶段: {phase_map.get(phase, phase)}")

    if s.get("winner"):
        label = {"good": "好人阵营", "evil": "狼人阵营", "draw": "平局"}.get(s["winner"], s["winner"])
        print(f"胜负: {label} 获胜")
    else:
        print(f"存活: {len(al)} 人 ({', '.join(al) if al else '无'})")

    if s.get("sheriff"):
        print(f"警长: {s['sheriff']}")

    if phase != "ended":
        total = len(flow_def)
        if step < total:
            action_id, name, hint = flow_def[step]
            print(f"当前步骤: {step+1}/{total} [{action_id}] {name}")
            print(f"操作提示: {hint}")
        else:
            print(f"当前步骤: {total}/{total} (全部完成)")
            phase_cmd = {"night": "night", "day": "day", "vote": "vote"}.get(phase, phase)
            print(f"提示: 执行 {phase_cmd} 命令提交结果")

        print("\n完整流程:")
        for i, (aid, aname, _) in enumerate(flow_def):
            mark = "V" if i < step else ">" if i == step else "."
            print(f"  {mark} [{aid}] {aname}")

    save_game(s)


def cmd_validate(args):
    s = load_game()
    cfg = load_config()
    phase = s.get("phase", "ended")
    rnd = s.get("round", 1)
    al = alive(s)
    issues = []

    if phase == "ended":
        print("游戏已结束，无需校验")
        return

    if phase == "night":
        guard_alive = any(p["role"] == "guard" and p["alive"] for p in s["players"].values())
        guarded = any(p.get("guard_protected") for p in s["players"].values())
        if guard_alive and not guarded:
            issues.append("守卫未行动（可能已空过，可忽略）")

        kill_in_log = any("狼人刀了" in e for e in s.get("log", []))
        if not kill_in_log:
            issues.append("狼人未决定刀人目标")

        witch = next((n for n, p in s["players"].items() if p["role"] == "witch" and p["alive"]), None)
        if witch:
            w = s["players"][witch]
            if not w.get("witch_acted_tonight"):
                has_antidote = not w.get("witch_antidote_used")
                has_poison = not w.get("witch_poison_used")
                if has_antidote or has_poison:
                    issues.append(f"女巫({witch})有药未使用")

        check_in_log = any("预言家查验" in e for e in s.get("log", []))
        seer_alive = any(p["role"] == "seer" and p["alive"] for p in s["players"].values())
        if seer_alive and not check_in_log:
            issues.append("预言家未查验")

    elif phase == "day":
        if rnd == 1 and not s.get("sheriff"):
            issues.append("第1天未选举警长")

        day_speeches = sum(
            1 for p in s["players"].values()
            for sp in p.get("speeches", [])
            if sp.startswith(f"Day{rnd}:")
        )
        if day_speeches < len(al):
            missing = len(al) - day_speeches
            issues.append(f"有 {missing}/{len(al)} 名存活玩家未发言")

        sheriff = s.get("sheriff")
        if sheriff and s["players"].get(sheriff, {}).get("alive") and not s.get("sheriff_direction"):
            issues.append(f"警长({sheriff})存活但未指定发言方向")

    elif phase == "vote":
        day_speeches = sum(
            1 for p in s["players"].values()
            for sp in p.get("speeches", [])
            if sp.startswith(f"Day{rnd}:")
        )
        if day_speeches < len(al):
            missing = len(al) - day_speeches
            issues.append(f"有 {missing}/{len(al)} 名存活玩家未发言就进入投票")

    if issues:
        print(f"发现 {len(issues)} 个潜在问题:")
        for issue in issues:
            print(f"  [!] {issue}")
    else:
        print("流程完整性检查通过，无问题")


def cmd_preview(args):
    s = load_game()
    phase = s.get("phase", "ended")
    rnd = s.get("round", 1)
    al = alive(s)
    flow_def, step = get_flow(s)

    if phase == "ended":
        print("游戏已结束")
        return

    phase_map = {"night": "夜晚", "day": "白天", "vote": "投票"}
    print(f"第 {rnd} 轮 {phase_map.get(phase, phase)} — 行动预览")
    print(f"存活玩家 ({len(al)}): {', '.join(al)}")

    for i, (aid, aname, ahint) in enumerate(flow_def):
        status = "DONE" if i < step else "NOW" if i == step else "TODO"
        print(f"\n  [{status}] 步骤 {i+1}: {aname} ({aid})")
        print(f"    操作: {ahint}")

        if aid == "wolf_proposal" and phase == "night":
            wl = [n for n in al if s["players"][n]["role"] in ("wolf", "mech_wolf")]
            if wl:
                print(f"    存活狼人: {', '.join(wl)}")
                print(f"    请每狼独立提案：刀谁 + 想悍跳/倒钩/冲锋")

        if aid == "wolf_discuss" and phase == "night":
            wl = [n for n in al if s["players"][n]["role"] in ("wolf", "mech_wolf")]
            if wl:
                print(f"    存活狼人: {', '.join(wl)}")
                print(f"    请所有狼看到彼此提案后，根据当前版面决定大致战术，讨论分歧")

        if aid == "wolf_vote" and phase == "night":
            if s.get("wolf_disagreement"):
                print(f"    有分歧，需要投票决定")
            else:
                print(f"    无分歧，可跳过")

        if aid == "wolf_assign" and phase == "night":
            print(f"    确认最终分工：谁悍跳 + 谁倒钩 + 刀谁 + 查杀目标")

        if aid == "guard" and phase == "night":
            guard = [n for n in al if s["players"][n]["role"] == "guard"]
            if guard:
                print(f"    守卫: {', '.join(guard)}")
            last = s.get("last_guard_target")
            if last:
                print(f"    上一晚守护: {last}")

        if aid == "witch_save" and phase == "night":
            witch = [n for n in al if s["players"][n]["role"] == "witch"]
            if witch:
                print(f"    女巫: {', '.join(witch)}")
                w = s["players"][witch[0]]
                antidote = "已用" if w.get("witch_antidote_used") else "可用"
                print(f"    解药: {antidote}")
                killed = [n for n in al if s["players"][n].get("killed_tonight")]
                if killed:
                    print(f"    被刀者: {', '.join(killed)}")

        if aid == "witch_poison" and phase == "night":
            witch = [n for n in al if s["players"][n]["role"] == "witch"]
            if witch:
                w = s["players"][witch[0]]
                poison = "已用" if w.get("witch_poison_used") else "可用"
                print(f"    毒药: {poison}")

        if aid == "seer" and phase == "night":
            seer = [n for n in al if s["players"][n]["role"] == "seer"]
            if seer:
                print(f"    预言家: {', '.join(seer)}")

        if aid == "sheriff" and phase == "day":
            print(f"    1. parallel_tasks问所有人是否上警")
            print(f"    2. task让候选人发言")
            print(f"    3. parallel_tasks收集警下投票")
            print(f"    4. 执行 sheriff 命令")

        if aid == "speech" and phase == "day":
            print(f"    需覆盖 {len(al)} 名存活玩家")

    phase_cmd = {"night": "night", "day": "day", "vote": "vote"}
    if step >= len(flow_def):
        print(f"\n  执行: {phase_cmd[phase]} 命令")

    save_game(s)


def cmd_init_flow(args):
    s = load_game()
    phase = s.get("phase", "ended")
    ensure_flow(s)
    save_game(s)
    print(f"已重置流程状态，当前阶段: {phase}")
    flow_def, step = get_flow(s)
    total = len(flow_def)
    print(f"当前进度: {step}/{total}")


def cmd_advance(args):
    """手动推进 flow step（用于GM完成当前步骤后标记）。"""
    s = load_game()
    phase = s.get("phase", "ended")
    flow_def, step = get_flow(s)
    f = ensure_flow(s)

    if phase == "ended":
        print("游戏已结束")
        return

    key = "night_step" if phase == "night" else "day_step"
    if step < len(flow_def):
        action_id, name, _ = flow_def[step]
        f[key] = step + 1
        mark_step_completed(s, action_id)
        print(f"已推进: [{action_id}] {name} → 完成")
        total = len(flow_def)
        if f[key] < total:
            next_id, next_name, _ = flow_def[f[key]]
            print(f"下一步: [{next_id}] {next_name}")
        else:
            phase_cmd = {"night": "night", "day": "day", "vote": "vote"}
            print(f"所有步骤完成，执行 {phase_cmd[phase]} 命令")
    else:
        print("所有步骤已完成，无需推进")


def main():
    import sys
    sys.stdout.reconfigure(encoding='utf-8')
    parser = argparse.ArgumentParser(description="游戏流程控制器")
    subparsers = parser.add_subparsers(dest="command", required=True)

    subparsers.add_parser("next", help="返回下一步操作")
    subparsers.add_parser("status", help="显示当前状态")
    subparsers.add_parser("validate", help="校验流程完整性")
    subparsers.add_parser("preview", help="预览当前阶段所有行动")
    subparsers.add_parser("init-flow", help="重置流程状态")
    adv = subparsers.add_parser("advance", help="推进到下一步")

    args = parser.parse_args(sys.argv[1:])

    command_map = {
        "next": cmd_next,
        "status": cmd_status,
        "validate": cmd_validate,
        "preview": cmd_preview,
        "init-flow": cmd_init_flow,
        "advance": cmd_advance,
    }

    handler = command_map.get(args.command)
    if handler:
        handler(args)
    else:
        parser.print_help()


if __name__ == "__main__":
    main()
