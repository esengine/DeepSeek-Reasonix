"""replay.py — cmd_replay"""

import json
from pathlib import Path

from state import load_game, LOG_FILE
from roles import ROLES


def cmd_replay(args):
    """生成完整的对局复盘MD文件。"""
    s = load_game()

    lines = []
    lines.append("# 狼人杀对局复盘")
    lines.append("")
    lines.append("## 基本信息")
    lines.append("")
    lines.append(f"- **玩家数量**: {len(s['players'])}人")
    lines.append(f"- **游戏结果**: {'好人胜' if s.get('winner') == 'good' else '狼人胜' if s.get('winner') == 'evil' else '平局' if s.get('winner') == 'draw' else '进行中'}")
    lines.append(f"- **总轮次**: {s['round']}轮")
    lines.append("")

    lines.append("## 角色分配")
    lines.append("")
    lines.append("| 玩家 | 角色 | 状态 |")
    lines.append("|------|------|------|")
    for name, p in sorted(s['players'].items(), key=lambda x: x[1]['role']):
        status = "存活" if p['alive'] else "死亡"
        lines.append(f"| {name} | {ROLES[p['role']]} | {status} |")
    lines.append("")

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
                wolves = data.get('wolves', [])
                target = data.get('target', '')
                lines.append(f"- **夜晚**: 狼人{'、'.join(wolves)}刀了{target}")
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
                result_cn = '好人' if result == 'villager' or result in ['seer', 'witch', 'hunter', 'guard'] else '狼人'
                lines.append(f"- **夜晚**: 预言家{seer}查验{target} = {result_cn}")
            elif event_type == 'night_death':
                dead = data.get('dead', [])
                lines.append(f"- **天亮**: 昨夜死亡 - {', '.join(dead)}")
            elif event_type == 'speech':
                name = data.get('name', '')
                text = data.get('text', '')
                lines.append(f"- **发言**: {name}: {text}")
            elif event_type == 'vote':
                name = data.get('name', '')
                target = data.get('target', '')
                lines.append(f"- **投票**: {name}投了{target}")
            elif event_type == 'execution':
                name = data.get('name', '')
                votes = data.get('votes', 0)
                lines.append(f"- **处决**: {name}被处决（{votes}票）")
            elif event_type == 'game_end':
                winner = data.get('winner', '')
                lines.append(f"- **游戏结束**: {'好人胜' if winner == 'good' else '狼人胜'}")
        lines.append("")

    lines.append("## 关键事件摘要")
    lines.append("")
    for log_entry in s.get("log", []):
        lines.append(f"- {log_entry}")
    lines.append("")

    output_file = args.output
    with open(output_file, 'w', encoding='utf-8') as f:
        f.write('\n'.join(lines))

    print(f"✅ 对局复盘已生成: {output_file}")
    print(f"📊 共 {len(events)} 个事件记录")
