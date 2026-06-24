"""replay.py — cmd_replay"""

import json
from pathlib import Path

from state import load_game, LOG_FILE
from roles import ROLES, is_wolf_role, wolves


def cmd_replay(args):
    """生成完整的对局复盘MD文件。支持 --annotate 模式带策略标注。"""
    s = load_game()
    cfg = {}  # Won't have full config but enough
    al = [n for n, p in s["players"].items() if p["alive"]]
    wolf_set = _get_wolf_names(s)

    lines = []
    lines.append("# 狼人杀对局复盘")
    lines.append("")

    # ── 基本信息 ──
    lines.append("## 基本信息")
    lines.append("")
    lines.append(f"- **玩家数量**: {len(s['players'])}人")
    winner_text = '好人胜' if s.get('winner') == 'good' else '狼人胜' if s.get('winner') == 'evil' else '平局' if s.get('winner') == 'draw' else '进行中'
    lines.append(f"- **游戏结果**: {winner_text}")
    lines.append(f"- **总轮次**: {s['round']}轮")
    if args.annotate:
        lines.append(f"- **狼队成员**: {'、'.join(wolf_set) if wolf_set else '无'}")
    lines.append("")

    # ── 角色分配（含阵营标记）──
    lines.append("## 角色分配")
    lines.append("")
    if args.annotate:
        lines.append("| 玩家 | 角色 | 阵营 | 状态 | 策略 |")
        lines.append("|------|------|------|------|------|")
    else:
        lines.append("| 玩家 | 角色 | 状态 |")
        lines.append("|------|------|------|")
    for name, p in sorted(s['players'].items(), key=lambda x: x[1]['role']):
        status = "存活" if p['alive'] else "死亡"
        camp = "🐺 狼队" if is_wolf_role(p['role']) else "👤 好人"
        if p['role'] in ("curse_fox",):
            camp = "🦊 第三方"
        if args.annotate:
            strategy = p.get('wolf_plans', [])
            strategy_text = ""
            if strategy:
                plans = []
                for sp in strategy:
                    s_info = sp.get('strategy', '')
                    if s_info:
                        plans.append(s_info)
                strategy_text = " → ".join(plans) if plans else ""
            lines.append(f"| {name} | {ROLES[p['role']]} | {camp} | {status} | {strategy_text} |")
        else:
            lines.append(f"| {name} | {ROLES[p['role']]} | {status} |")
    lines.append("")

    # ── 游戏日志（带策略标注）──
    lines.append("## 游戏日志")
    lines.append("")

    events = []
    if Path(LOG_FILE).exists():
        try:
            with open(LOG_FILE, encoding='utf-8') as f:
                for line in f:
                    try:
                        e = json.loads(line)
                        events.append(e)
                    except json.JSONDecodeError:
                        continue
        except OSError as e:
            print(f"[WARN] 日志文件读取失败: {e}")

        current_round = 0
        for e in sorted(events, key=lambda x: x.get('t', 0)):
            event_type = e.get('type')
            data = e.get('data', {})
            round_num = data.get('round', 0)

            if round_num != current_round:
                current_round = round_num
                lines.append(f"### 第{round_num}轮")
                lines.append("")

            if event_type == 'wolf_kill':
                w_list = data.get('wolves', [])
                target = data.get('target', '')
                is_wolf_target = target in wolf_set
                annotation = f" (刀中狼队友)" if is_wolf_target and args.annotate else ""
                lines.append(f"- **夜晚**: 狼人{'、'.join(w_list)}刀了{target}{annotation}")
            elif event_type == 'witch_save':
                target = data.get('target', '')
                lines.append(f"- **夜晚**: 女巫救了{target}")
            elif event_type == 'witch_poison':
                target = data.get('target', '')
                lines.append(f"- **夜晚**: 女巫毒了{target}")
            elif event_type == 'seer_check':
                seer = data.get('seer', '')
                target = data.get('target', '')
                result = data.get('result', '')
                result_cn = _role_to_camp(result)
                is_wolf = target in wolf_set
                annotation = f" 🔍(狼人身份)" if is_wolf and args.annotate else ""
                lines.append(f"- **夜晚**: 预言家{seer}查验{target} = {result_cn}{annotation}")
            elif event_type == 'curse_fox_death':
                target = data.get('target', '')
                lines.append(f"- **夜晚**: 🦊 咒狐 {target} 被预言家查验反噬而死！")
            elif event_type == 'night_death':
                dead = data.get('dead', [])
                wolf_dead = [n for n in dead if n in wolf_set]
                good_dead = [n for n in dead if n not in wolf_set]
                parts = []
                if good_dead:
                    parts.append(f"好方 {'、'.join(good_dead)}")
                if wolf_dead:
                    parts.append(f"狼方 {'、'.join(wolf_dead)}")
                annotation = f"（{'、'.join(parts)}）" if parts and args.annotate else ""
                lines.append(f"- **天亮**: 昨夜死亡 - {', '.join(dead)} {annotation}")
            elif event_type == 'speech':
                name = data.get('name', '')
                text = data.get('text', '')
                marker = "🐺 " if name in wolf_set and args.annotate else ""
                lines.append(f"- **发言**: {marker}{name}: {text}")
            elif event_type == 'vote':
                name = data.get('name', '')
                target = data.get('target', '')
                voter_marker = "🐺" if name in wolf_set and args.annotate else "👤"
                target_marker = "🐺" if target in wolf_set and args.annotate else "👤"
                if args.annotate:
                    lines.append(f"- **投票**: {voter_marker} {name} → {target_marker} {target}")
                else:
                    lines.append(f"- **投票**: {name}投了{target}")
            elif event_type == 'execution':
                name = data.get('name', '')
                votes = data.get('votes', 0)
                is_wolf_executed = name in wolf_set
                annotation = " 🐺(狼人被投出)" if is_wolf_executed and args.annotate else " 👤(好人被投出)" if not is_wolf_executed and args.annotate else ""
                lines.append(f"- **处决**: {name}被处决（{votes}票）{annotation}")
            elif event_type == 'game_end':
                winner = data.get('winner', '')
                lines.append(f"- **游戏结束**: {'好人胜' if winner == 'good' else '狼人胜'}")
        lines.append("")

    # ── 关键事件摘要（总是包含）──
    lines.append("## 关键事件摘要")
    lines.append("")
    for log_entry in s.get("log", []):
        lines.append(f"- {log_entry}")
    lines.append("")

    # ── 策略分析（仅 --annotate）──
    if args.annotate:
        lines.append("## 策略分析")
        lines.append("")
        for name, p in sorted(s['players'].items()):
            if is_wolf_role(p['role']):
                plans = p.get('wolf_plans', [])
                role_name = ROLES.get(p['role'], p['role'])
                if plans:
                    lines.append(f"### 🐺 {name}（{role_name}）策略")
                    lines.append("")
                    for i, sp in enumerate(plans, 1):
                        strategy = sp.get('strategy', '未记录')
                        claim = sp.get('claim', '')
                        check = sp.get('check', '')
                        parts = [f"轮次{sp.get('round', '?')}: {strategy}"]
                        if claim:
                            parts.append(f"悍跳目标={claim}")
                        if check:
                            parts.append(f"查验目标={check}")
                        lines.append(f"- {'、'.join(parts)}")
                    lines.append("")

        # 狼队配合分析
        wolf_names = wolf_set
        if len(wolf_names) > 1:
            lines.append("### 狼队配合")
            lines.append("")
            strategies = {}
            for n in wolf_names:
                plans = s['players'][n].get('wolf_plans', [])
                for sp in plans:
                    strat = sp.get('strategy', '')
                    if strat:
                        strategies.setdefault(strat, []).append(n)
            if strategies:
                for strat, players in strategies.items():
                    lines.append(f"- **{strat}**: {'、'.join(players)}")
                lines.append("")

    output_file = args.output
    with open(output_file, 'w', encoding='utf-8') as f:
        f.write('\n'.join(lines))

    print(f"✅ 对局复盘已生成: {output_file}")
    print(f"📊 共 {len(events)} 个事件记录")
    if args.annotate:
        print(f"🔍 策略标注模式已启用")


def _get_wolf_names(s):
    """获取所有狼队成员名字。"""
    return [n for n, p in s['players'].items() if is_wolf_role(p['role'])]


def _role_to_camp(role):
    """角色 → 阵营标注。"""
    if role in ("villager",):
        return "村民"
    if role in ("seer", "witch", "hunter", "guard", "idiot"):
        return "神牌"
    if is_wolf_role(role):
        return "狼人"
    return role
