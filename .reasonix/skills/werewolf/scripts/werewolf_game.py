#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""狼人杀游戏引擎 — 主入口。import + 参数解析 + 命令分发。"""
import io, sys, argparse

sys.stdout = io.TextIOWrapper(sys.stdout.buffer, encoding="utf-8")

from state import load_game, save_game
from roles import ROLES, ROLE_ICONS, load_config
from commands import (
    cmd_init, cmd_night, cmd_night_auto, cmd_day, cmd_day_auto,
    cmd_vote, cmd_explode, cmd_hunter_shot, cmd_sheriff, cmd_sheriff_direction,
    cmd_config, cmd_status, cmd_status_pretty, cmd_summary,
    cmd_stats, cmd_hint, cmd_journal, cmd_reset, cmd_make_prompts, cmd_replay,
    cmd_log_raw,
)

def cmd_save_wolf_plans(args):
    """保存狼人策略分配到玩家状态。"""
    s = load_game()
    player = args.player
    if player not in s["players"]:
        print(f"[!] 玩家 {player} 不存在")
        return
    if s["players"][player]["role"] not in ("wolf", "mech_wolf"):
        print(f"[!] {player} 不是狼人")
        return
    
    plan = {
        "round": s["round"],
        "strategy": args.strategy or "",
        "claim": args.claim or "",
        "check": args.check or "",
    }
    
    if "wolf_plans" not in s["players"][player]:
        s["players"][player]["wolf_plans"] = []
    s["players"][player]["wolf_plans"].append(plan)
    save_game(s)
    print(f"[W] 已保存 {player} 的策略分配: {plan}")

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="狼人杀游戏引擎")
    sub = parser.add_subparsers(dest="cmd")

    p_init = sub.add_parser("init")
    p_init.add_argument("players", nargs="+")

    p_night = sub.add_parser("night")
    p_night.add_argument("--kill")
    p_night.add_argument("--guard")
    p_night.add_argument("--check")
    p_night.add_argument("--save", action="store_true")
    p_night.add_argument("--no-save", action="store_true", help="女巫确认不救")
    p_night.add_argument("--poison")
    p_night.add_argument("--extra-kill")
    p_night.add_argument("--mimic-action", help="模仿狼夜间行动目标")
    p_night.add_argument("--pass-sheriff", help="警长死亡时传给谁")
    p_night.add_argument("--no-sheriff-confirm", action="store_true", help="跳过警长传位确认")
    p_night.add_argument("--hunter-target", help="猎人夜间被刀的开枪目标")
    p_night.add_argument("--no-shot-warn", action="store_true", help="跳过无猎人可开枪的警告")
    p_night.add_argument("--last-words", nargs="*", default=[], help="夜间死亡遗言 名字:内容")

    p_night_auto = sub.add_parser("night-auto")
    p_night_auto.add_argument("--wolf", nargs="*", default=[])
    p_night_auto.add_argument("--guard")
    p_night_auto.add_argument("--seer")
    p_night_auto.add_argument("--witch")
    p_night_auto.add_argument("--extra-kill")
    p_night_auto.add_argument("--mimic-role", help="模仿狼选择身份模板 (seer/witch/hunter/guard/villager/wolf) 仅第1晚")
    p_night_auto.add_argument("--mimic-action", help="模仿狼夜间行动（查验/守护目标）")
    p_night_auto.add_argument("--mimic-witch", help="模仿狼(女巫)救毒回复")
    p_night_auto.add_argument("--pass-sheriff", help="警长死亡时传给谁")
    p_night_auto.add_argument("--no-sheriff-confirm", action="store_true", help="跳过警长传位确认")
    p_night_auto.add_argument("--hunter", help="猎人夜间被刀的开枪原始回复")
    p_night_auto.add_argument("--last-words", nargs="*", default=[], help="首夜遗言 名字:内容")
    p_night_auto.add_argument("--dry-run", action="store_true", help="预览今夜行动而不执行")
    p_night_auto.add_argument("--collect", action="store_true", help="只收集AI回复，不执行（同--dry-run）")

    p_day = sub.add_parser("day")
    p_day.add_argument("--speech", nargs="*", default=[])
    p_day.add_argument("--vote", nargs="*", default=[])
    p_day.add_argument("--last-words", nargs="*", default=[], help="遗言 名字:内容")
    p_day.add_argument("--hunter-target")
    p_day.add_argument("--mechwolf-target")
    p_day.add_argument("--pass-sheriff", help="警长被处决时传给谁")
    p_day.add_argument("--idiot-reveal", action="store_true", help="白痴被投时选择翻牌")
    p_day.add_argument("--no-sheriff", action="store_true", help="确认第1天不选警长，直接投票")
    p_day.add_argument("--start-from", choices=["左", "右"], help="警长决定发言起始方向")
    p_day.add_argument("--white-wolf-target", nargs="*", default=[], help="白狼王被投带走目标 目标1 目标2")

    p_day_auto = sub.add_parser("day-auto")
    p_day_auto.add_argument("--speech", nargs="*", default=[])
    p_day_auto.add_argument("--vote", nargs="*", default=[])
    p_day_auto.add_argument("--hunter")
    p_day_auto.add_argument("--mechwolf")
    p_day_auto.add_argument("--white-wolf", nargs="*", default=[], help="白狼王被投带走目标 目标1 目标2")
    p_day_auto.add_argument("--last-words", nargs="*", default=[])
    p_day_auto.add_argument("--pass-sheriff", help="警长被处决时传给谁")
    p_day_auto.add_argument("--no-sheriff-confirm", action="store_true", help="跳过警长传位确认")
    p_day_auto.add_argument("--no-sheriff", action="store_true", help="确认第1天不选警长，直接投票")
    p_day_auto.add_argument("--idiot-reveal", action="store_true", help="白痴被投时选择翻牌")
    p_day_auto.add_argument("--start-from", choices=["左", "右"], help="警长决定发言起始方向")

    p_vote = sub.add_parser("vote")
    p_vote.add_argument("--vote", nargs="*", default=[], help="投票回复 玩家:任意回复")
    p_vote.add_argument("--hunter", help="猎人开枪原始回复")
    p_vote.add_argument("--idiot-reveal", action="store_true")
    p_vote.add_argument("--no-speech", action="store_true", help="跳过发言不足警告")
    p_vote.add_argument("--last-words", nargs="*", default=[], help="遗言 名字:内容")
    p_vote.add_argument("--pass-sheriff", help="警长被处决时传给谁")
    p_vote.add_argument("--no-sheriff-confirm", action="store_true", help="跳过警长传位确认")
    p_vote.add_argument("--start-from", choices=["左", "右"], help="警长决定发言起始方向")
    p_vote.add_argument("--white-wolf", nargs="*", default=[], help="白狼王被投带走目标 目标1 目标2")

    p_explode = sub.add_parser("explode")
    p_explode.add_argument("name")
    p_explode.add_argument("--confirmed", action="store_true", help="已确认狼人同意自爆")

    p_hunter_shot = sub.add_parser("hunter-shot")
    p_hunter_shot.add_argument("shooter", help="开枪的猎人")
    p_hunter_shot.add_argument("target", help="被带走的目标")
    p_hunter_shot.add_argument("--confirmed", action="store_true", help="已确认猎人同意开枪")

    p_sheriff = sub.add_parser("sheriff")
    p_sheriff.add_argument("--candidates", nargs="*", default=[], help="上警候选人")
    p_sheriff.add_argument("--withdrew", nargs="*", default=[], help="退水玩家")
    p_sheriff.add_argument("--pk", action="store_true", help="PK投票模式")
    p_sheriff.add_argument("--vote", nargs="*", default=[], help="投票 投票人:候选人")

    p_sheriff_dir = sub.add_parser("sheriff-direction")
    p_sheriff_dir.add_argument("direction", choices=["左", "右"], help="发言起始方向")

    p_config = sub.add_parser("config")
    p_config.add_argument("--show", action="store_true")
    p_config.add_argument("key", nargs="?")
    p_config.add_argument("value", nargs="?")

    p_status = sub.add_parser("status")
    p_status_pretty = sub.add_parser("status-pretty")
    p_status_pretty.add_argument("--roles", action="store_true")

    p_summary = sub.add_parser("summary")
    p_summary.add_argument("--for-player", help="只显示该玩家应知的信息")
    p_summary.add_argument("--with-history", action="store_true",
        help="追加历史事件/发言/投票记录（同轮子agent共享前缀，触发缓存命中）")

    p_stats = sub.add_parser("stats")
    p_stats.add_argument("--pretty", action="store_true", help="详细统计面板")

    p_hint = sub.add_parser("hint")
    p_hint.add_argument("role", help="角色: wolf/seer/witch/hunter/guard/idiot/villager/mech_wolf")

    p_journal = sub.add_parser("journal")

    p_reset = sub.add_parser("reset")

    p_replay = sub.add_parser("replay")
    p_replay.add_argument("--output", default="replay.md", help="输出文件名 (默认: replay.md)")

    p_log_raw = sub.add_parser("log-raw")
    p_log_raw.add_argument("type", choices=["speech","vote","wolf_kill","witch","seer","guard","sheriff","mimic","hunter","last_words","wolf_strategy","wolf_adjust"])
    p_log_raw.add_argument("player", help="AI 玩家名称")
    p_log_raw.add_argument("text", help="AI 原始回复文本")

    p_prompts = sub.add_parser("make-prompts")
    p_prompts.add_argument("action", choices=["speech","vote","night_kill","night_check","night_guard","witch","sheriff","sheriff_withdraw","last_words","hunter_active","idiot_reveal","wolf_explode","mimic","wolf_adjust","wolf_strategy","wolf_claim","wolf_hunting","wolf_deep","good_day","god_hide"],
                          help="prompt 类型")
    p_prompts.add_argument("--with-history", action="store_true", help="包含历史事件和发言记录")
    p_prompts.add_argument("--depth", choices=["basic", "analysis", "game-theory"], default="basic",
                          help="思维链层级: basic=信息层, analysis=+分析层, game-theory=+博弈层")
    p_prompts.add_argument("--player-count", type=int, help="总玩家数（用于策略调整，默认=当前存活+死亡）")
    p_prompts.add_argument("--for-player", type=str, help="只输出指定玩家的prompt")
    p_prompts.add_argument("--kill", type=str, help="女巫专属：今晚被刀的人名")

    p_save_wolf_plans = sub.add_parser("save-wolf-plans")
    p_save_wolf_plans.add_argument("player", help="狼人玩家名称")
    p_save_wolf_plans.add_argument("--strategy", help="角色: 悍跳/倒钩/深水/冲锋")
    p_save_wolf_plans.add_argument("--claim", help="悍跳目标")
    p_save_wolf_plans.add_argument("--check", help="查验目标")

    args = parser.parse_args()
    if args.cmd == "init":
        cmd_init(args)
    elif args.cmd == "night":
        cmd_night(args)
    elif args.cmd == "night-auto":
        cmd_night_auto(args)
    elif args.cmd == "day":
        cmd_day(args)
    elif args.cmd == "day-auto":
        cmd_day_auto(args)
    elif args.cmd == "vote":
        cmd_vote(args)
    elif args.cmd == "explode":
        cmd_explode(args)
    elif args.cmd == "hunter-shot":
        cmd_hunter_shot(args)
    elif args.cmd == "sheriff":
        cmd_sheriff(args)
    elif args.cmd == "sheriff-direction":
        cmd_sheriff_direction(args)
    elif args.cmd == "config":
        cmd_config(args)
    elif args.cmd == "status":
        cmd_status(args)
    elif args.cmd == "status-pretty":
        cmd_status_pretty(args)
    elif args.cmd == "summary":
        cmd_summary(args)
    elif args.cmd == "stats":
        cmd_stats(args)
    elif args.cmd == "hint":
        cmd_hint(args)
    elif args.cmd == "journal":
        cmd_journal(args)
    elif args.cmd == "reset":
        cmd_reset(args)
    elif args.cmd == "replay":
        cmd_replay(args)
    elif args.cmd == "log-raw":
        cmd_log_raw(args)
    elif args.cmd == "make-prompts":
        cmd_make_prompts(args)
    elif args.cmd == "save-wolf-plans":
        cmd_save_wolf_plans(args)
    else:
        parser.print_help()
