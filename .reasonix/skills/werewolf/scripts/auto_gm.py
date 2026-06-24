#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""auto_gm.py — 半自动GM，自动化游戏流程"""

import sys, json, subprocess, argparse
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
from state import load_game, save_game
from game_flow import get_flow, ensure_flow, NIGHT_FLOW, DAY_FLOW, VOTE_FLOW
from prompt_builder import PromptBuilder
from response_parser import ResponseParser
from roles import alive, wolves as get_wolves
from utils import extract_name


class AutoGameMaster:
    """半自动GM"""

    def __init__(self, state_path=None, config_path=None):
        self.scripts_dir = Path(__file__).parent
        self.project_root = self.scripts_dir.parent.parent.parent.parent
        self._state_path = state_path or str(self.project_root / "werewolf_state.json")
        self._config_path = config_path or str(self.project_root / "werewolf_config.json")
        self._prompt_builder = PromptBuilder(state_path=self._state_path, config_path=self._config_path)
        self._response_parser = ResponseParser(state_path=self._state_path)

    def _reload(self):
        self._prompt_builder.reload()
        self._response_parser.reload()

    def _game_cmd(self, *args):
        script = self.scripts_dir / "werewolf_game.py"
        cmd = [sys.executable, str(script)] + list(args)
        result = subprocess.run(cmd, capture_output=True, encoding="utf-8", errors="replace")
        out = result.stdout.strip()
        if result.stderr:
            out += "\n" + result.stderr.strip()
        if result.returncode != 0:
            out = f"[ERROR] 返回码 {result.returncode}\n{out}"
        return out

    def _flow_cmd(self, *args):
        script = self.scripts_dir / "game_flow.py"
        cmd = [sys.executable, str(script)] + list(args)
        result = subprocess.run(cmd, capture_output=True, encoding="utf-8", errors="replace")
        return result.stdout.strip() if result.stdout else ""

    def start(self, players: list) -> str:
        result = self._game_cmd("init", *players)
        self._reload()
        return result

    def _prompt_and_input(self, action: str, player: str, label: str = None) -> str:
        prompt = self._prompt_builder.build(action, player)
        l = label or player
        print(f"\n--- {l} ---")
        print(prompt)
        return input(f"[{l}] 请输入回复: ").strip()

    def _collect_night_data(self):
        s = load_game()
        al = alive(s)
        data = {"wolf": [], "guard": None, "seer": None, "witch": None}

        guard = [n for n in al if s["players"][n]["role"] == "guard"]
        if guard:
            print(f"\n{'='*60}")
            print(f"[1/{len(NIGHT_FLOW)}] 守卫守护")
            resp = self._prompt_and_input("night-guard", guard[0])
            parsed = self._response_parser.parse_guard(resp) if resp else None
            data["guard"] = parsed if parsed else (resp if resp else None)
            print(f"  → 守卫: {data['guard'] or '空过'}")

        wolves = get_wolves(s)
        if wolves:
            print(f"\n{'='*60}")
            print(f"[2/{len(NIGHT_FLOW)}] 狼人刀人")
            for w in wolves:
                resp = self._prompt_and_input("night-wolf", w)
                if resp:
                    data["wolf"].append(f"{w}:{resp}")
            if data["wolf"]:
                parsed = self._response_parser.parse_wolf_kill(data["wolf"])
                if parsed.get("tie"):
                    print(f"  → 狼人平票: {', '.join(parsed['selected'])}（需重新讨论）")
                elif parsed.get("selected"):
                    print(f"  → 狼人决议: 刀 {parsed['selected'][0]}")
            else:
                print("  → 无狼人回复")

        witch = [n for n in al if s["players"][n]["role"] == "witch"]
        if witch:
            print(f"\n{'='*60}")
            print(f"[3/{len(NIGHT_FLOW)}] 女巫行动")
            w = witch[0]
            wp = s["players"][w]
            antidote = "已用" if wp.get("witch_antidote_used") else "可用"
            poison = "已用" if wp.get("witch_poison_used") else "可用"
            print(f"  [{w}] 解药({antidote}) 毒药({poison})")
            resp = self._prompt_and_input("night-witch", w)
            if resp:
                data["witch"] = resp
                save, pt = self._response_parser.parse_witch_action(resp)
                parts = ["救" if save else "不救"]
                if pt:
                    parts.append(f"毒{pt}")
                print(f"  → 女巫: {' '.join(parts)}")

        seer = [n for n in al if s["players"][n]["role"] == "seer"]
        if seer:
            print(f"\n{'='*60}")
            print(f"[4/{len(NIGHT_FLOW)}] 预言家查验")
            se = seer[0]
            resp = self._prompt_and_input("night-seer", se)
            target = self._response_parser.parse_seer_check(resp) if resp else None
            data["seer"] = target if target else resp
            print(f"  → 查验: {data['seer']}")

        return data

    def run_night(self) -> str:
        s = load_game()
        if s.get("phase") != "night":
            return f"[ERROR] 当前阶段不是夜晚（{s.get('phase')}），无法执行 run-night"

        print(f"{'='*60}")
        print(f"第 {s['round']} 晚 — 自动流程开始")
        print(f"{'='*60}")
        self._flow_cmd("init-flow")

        data = self._collect_night_data()

        args = ["night-auto"]
        for w in data["wolf"]:
            args.extend(["--wolf", w])
        if data["guard"]:
            args.extend(["--guard", data["guard"]])
        if data["seer"]:
            args.extend(["--seer", data["seer"]])
        if data["witch"]:
            args.extend(["--witch", data["witch"]])

        print(f"\n{'='*60}")
        print("▶ 执行夜间结算...")
        result = self._game_cmd(*args)
        self._reload()
        print(result)
        return result

    def _collect_speeches(self) -> list:
        s = load_game()
        al_players = alive(s)
        speeches = []
        print(f"\n{'='*60}")
        print("▶ 发言环节")
        for name in al_players:
            resp = self._prompt_and_input("day-speech", name)
            if resp:
                speeches.append(f"{name}:{resp}")
        print(f"  → 已收集 {len(speeches)}/{len(al_players)} 份发言")
        return speeches

    def _collect_votes(self) -> list:
        s = load_game()
        al_players = alive(s)
        votes = []
        print(f"\n{'='*60}")
        print("▶ 投票环节")
        for name in al_players:
            resp = self._prompt_and_input("day-vote", name)
            if resp:
                votes.append(f"{name}:{resp}")
        print(f"  → 已收集 {len(votes)}/{len(al_players)} 票")
        return votes

    def _step_sheriff(self) -> str:
        s = load_game()
        al_players = alive(s)

        print(f"\n{'='*60}")
        print("▶ 警长竞选")
        responses = {}
        for name in al_players:
            resp = self._prompt_and_input("sheriff", name)
            responses[name] = resp

        candidates = [n for n, r in responses.items() if r and ("上警" in r or "参选" in r or "竞选" in r or r.strip() in ("上", "是"))]
        if not candidates:
            print("  → 无人上警，警徽流失")
            self._game_cmd("sheriff")
            return "无人上警"

        print(f"  → 警上候选人: {', '.join(candidates)}")

        print("\n--- 候选人发言 ---")
        candidate_speeches = {}
        for c in candidates:
            speech = input(f"[{c}] 请输入发言: ").strip()
            candidate_speeches[c] = speech

        non_candidates = [n for n in al_players if n not in candidates]
        votes = {}
        if non_candidates:
            print("\n--- 警下投票 ---")
            for n in non_candidates:
                vote = input(f"[{n}] 投给谁?: ").strip()
                target = extract_name(vote, candidates)
                if target:
                    votes[n] = target

        vote_args = [f"{v}:{t}" for v, t in votes.items()]
        sheriff_result = self._game_cmd("sheriff", "--candidates", *candidates, "--vote", *vote_args)
        print(sheriff_result)
        return sheriff_result

    def run_day(self) -> str:
        s = load_game()
        if s.get("phase") not in ("day", "vote"):
            return f"[ERROR] 当前阶段不是白天（{s.get('phase')}），无法执行 run-day"

        print(f"{'='*60}")
        print(f"第 {s['round']} 天 — 自动流程开始")
        print(f"{'='*60}")
        self._flow_cmd("init-flow")

        if s.get("phase") == "day":
            if s["round"] == 1 and not s.get("sheriff"):
                self._step_sheriff()
                self._reload()
                s = load_game()

            if s.get("phase") == "day" and not s.get("winner"):
                speeches = self._collect_speeches()
                votes = self._collect_votes()

                args = ["day-auto"]
                for sp in speeches:
                    args.extend(["--speech", sp])
                for v in votes:
                    args.extend(["--vote", v])

                print(f"\n{'='*60}")
                print("▶ 执行白天结算...")
                result = self._game_cmd(*args)
                self._reload()
                print(result)
                return result

        elif s.get("phase") == "vote":
            votes = self._collect_votes()
            args = ["vote"]
            for v in votes:
                args.extend(["--vote", v])
            print(f"\n{'='*60}")
            print("▶ 执行投票结算...")
            result = self._game_cmd(*args)
            self._reload()
            print(result)
            return result

        return "无需执行白天的操作"

    def run_round(self) -> str:
        results = []
        print(f"{'='*60}")
        print("▶ 开始一轮（夜晚 + 白天）")
        print(f"{'='*60}")

        r = self.run_night()
        results.append(r)

        s = load_game()
        if not s.get("winner") and s.get("phase") in ("day", "vote"):
            r = self.run_day()
            results.append(r)

        return "\n---\n".join(results)

    def execute_next(self, user_input: str = None) -> str:
        s = load_game()
        phase = s.get("phase", "ended")

        if phase == "ended":
            winner = s.get("winner", "unknown")
            label = {"good": "好人阵营", "evil": "狼人阵营", "draw": "平局"}.get(winner, winner)
            return f"游戏已结束，{label} 获胜"

        flow_def, step = get_flow(s)

        if step >= len(flow_def):
            phase_map = {"night": "night", "day": "day", "vote": "vote"}
            return f"当前阶段所有步骤已完成，请执行 {phase_map.get(phase, phase)} 命令提交结果"

        action_id, name, hint = flow_def[step]
        round_num = s.get("round", 1)
        prefix = f"第 {round_num} {'晚' if phase == 'night' else '天'}"

        print(f"[{prefix}] 步骤 {step+1}/{len(flow_def)}: {name}")
        print(f"  [{action_id}] {hint}")

        if action_id in ("guard", "guard_proposal", "guard_vote"):
            guard = [n for n in alive(s) if s["players"][n]["role"] == "guard"]
            if guard:
                return self._handle_step_guard(guard[0], user_input)
            return "无存活守卫，跳过"

        if action_id.startswith("wolf"):
            wolves = get_wolves(s)
            if wolves:
                return self._handle_step_wolf(wolves, user_input)
            return "无存活狼人，跳过"

        if action_id.startswith("witch"):
            witch = [n for n in alive(s) if s["players"][n]["role"] == "witch"]
            if witch:
                return self._handle_step_witch(witch[0], user_input)
            return "无存活女巫，跳过"

        if action_id == "seer":
            seer = [n for n in alive(s) if s["players"][n]["role"] == "seer"]
            if seer:
                return self._handle_step_seer(seer[0], user_input)
            return "无存活预言家，跳过"

        if action_id == "sheriff":
            self._step_sheriff()
            self._advance_flow(s)
            return "警长竞选完成"

        if action_id == "speech":
            return self._handle_step_speech(user_input)

        if action_id == "vote":
            return self._handle_step_vote(user_input)

        if action_id == "execute":
            f = ensure_flow(s)
            phase_key = "night_step" if phase == "night" else "day_step"
            f[phase_key] = step + 1
            save_game(s)
            return "投票结果已自动执行，无需额外操作"

        return f"未知步骤: {action_id}"

    def _advance_flow(self, s):
        phase = s.get("phase", "ended")
        flow_def, step = get_flow(s)
        f = ensure_flow(s)
        key = "night_step" if phase == "night" else "day_step"
        if step < len(flow_def):
            f[key] = step + 1
        save_game(s)

    def _handle_step_guard(self, player: str, user_input: str = None) -> str:
        if not user_input:
            prompt = self._prompt_builder.build("night-guard", player)
            return f"请提供守卫回复\n{prompt}"
        parsed = self._response_parser.parse_guard(user_input)
        target = parsed if parsed else user_input
        s = load_game()
        self._advance_flow(s)

        result = self._game_cmd("night-auto", "--guard", str(target) if target else "空过")
        self._reload()
        return f"守卫守护: {target or '空过'}\n{result}"

    def _handle_step_wolf(self, players: list, user_input: str = None) -> str:
        if not user_input:
            prompts = []
            for p in players:
                prompt = self._prompt_builder.build("night-wolf", p)
                prompts.append(f"--- {p} ---\n{prompt}")
            return "请提供所有狼人回复（用 | 分隔）\n" + "\n".join(prompts)

        wolf_inputs = [u.strip() for u in user_input.split("|")]
        # 长度校验
        if len(wolf_inputs) != len(players):
            return f"[ERROR] 输入数量不匹配：需要 {len(players)} 个狼人回复，收到 {len(wolf_inputs)} 个\n用 | 分隔，格式：狼A回复|狼B回复|狼C回复"
        wolf_args = []
        for w, inp in zip(players, wolf_inputs):
            if inp:
                wolf_args.append(f"{w}:{inp}")

        s = load_game()
        self._advance_flow(s)
        args_list = []
        for wa in wolf_args:
            args_list.extend(["--wolf", wa])
        result = self._game_cmd("night-auto", *args_list)
        self._reload()
        return f"狼人行动完成\n{result}"

    def _handle_step_witch(self, player: str, user_input: str = None) -> str:
        if not user_input:
            prompt = self._prompt_builder.build("night-witch", player)
            return f"请提供女巫回复\n{prompt}"
        s = load_game()
        self._advance_flow(s)
        result = self._game_cmd("night-auto", "--witch", user_input)
        self._reload()
        save, pt = self._response_parser.parse_witch_action(user_input)
        parts = ["救" if save else "不救"]
        if pt:
            parts.append(f"毒{pt}")
        return f"女巫: {' '.join(parts)}\n{result}"

    def _handle_step_seer(self, player: str, user_input: str = None) -> str:
        if not user_input:
            prompt = self._prompt_builder.build("night-seer", player)
            return f"请提供预言家回复\n{prompt}"
        target = self._response_parser.parse_seer_check(user_input)
        s = load_game()
        self._advance_flow(s)
        result = self._game_cmd("night-auto", "--seer", target or user_input)
        self._reload()
        return f"预言家查验: {target or user_input}\n{result}"

    def _handle_step_speech(self, user_input: str = None) -> str:
        if not user_input:
            s = load_game()
            al_players = alive(s)
            prompts = []
            for name in al_players:
                prompt = self._prompt_builder.build("day-speech", name)
                prompts.append(f"--- {name} ---\n{prompt}")
            return "请提供所有玩家发言（用 | 分隔）\n" + "\n".join(prompts)

        s = load_game()
        al_players = alive(s)
        speech_inputs = [u.strip() for u in user_input.split("|")]
        # 长度校验
        if len(speech_inputs) != len(al_players):
            return f"[ERROR] 输入数量不匹配：需要 {len(al_players)} 个玩家发言，收到 {len(speech_inputs)} 个\n用 | 分隔，格式：玩家A发言|玩家B发言|..."
        speeches = []
        for name, inp in zip(al_players, speech_inputs):
            if inp:
                speeches.append(f"{name}:{inp}")

        self._advance_flow(s)
        args_list = []
        for sp in speeches:
            args_list.extend(["--speech", sp])
        if not args_list:
            return "无发言数据"
        result = self._game_cmd("day-auto", *args_list)
        self._reload()
        return f"发言收集完成\n{result}"

    def _handle_step_vote(self, user_input: str = None) -> str:
        if not user_input:
            s = load_game()
            al_players = alive(s)
            prompts = []
            for name in al_players:
                prompt = self._prompt_builder.build("day-vote", name)
                prompts.append(f"--- {name} ---\n{prompt}")
            return "请提供所有玩家投票（用 | 分隔）\n" + "\n".join(prompts)

        s = load_game()
        al_players = alive(s)
        vote_inputs = [u.strip() for u in user_input.split("|")]
        # 长度校验
        if len(vote_inputs) != len(al_players):
            return f"[ERROR] 输入数量不匹配：需要 {len(al_players)} 个玩家投票，收到 {len(vote_inputs)} 个\n用 | 分隔，格式：玩家A投X|玩家B投Y|..."
        votes = []
        for name, inp in zip(al_players, vote_inputs):
            if inp:
                votes.append(f"{name}:{inp}")

        self._advance_flow(s)
        args_list = []
        for v in votes:
            args_list.extend(["--vote", v])
        if not args_list:
            return "无投票数据"
        result = self._game_cmd("day-auto", *args_list)
        self._reload()
        return f"投票完成\n{result}"

    def get_status(self) -> dict:
        s = load_game()
        al = alive(s)
        phase = s.get("phase", "ended")
        flow_def, step = get_flow(s)

        phase_labels = {"night": "夜晚", "day": "白天", "vote": "投票", "ended": "已结束"}
        status = {
            "round": s.get("round", 1),
            "phase": phase,
            "phase_label": phase_labels.get(phase, phase),
            "alive_count": len(al),
            "alive_players": al,
            "sheriff": s.get("sheriff"),
            "winner": s.get("winner"),
            "winner_label": {"good": "好人阵营", "evil": "狼人阵营", "draw": "平局"}.get(s.get("winner"), ""),
            "total_players": len(s.get("players", {})),
            "log_count": len(s.get("log", [])),
            "flow_step": step,
            "flow_total": len(flow_def),
        }

        if phase != "ended" and step < len(flow_def):
            action_id, name, hint = flow_def[step]
            status["current_action"] = action_id
            status["current_step_name"] = name
            status["current_step_hint"] = hint

        return status


def cmd_start(gm, args):
    result = gm.start(args.players)
    print(result)


def cmd_run_night(gm, args):
    result = gm.run_night()
    print(result)


def cmd_run_day(gm, args):
    result = gm.run_day()
    print(result)


def cmd_run_round(gm, args):
    result = gm.run_round()
    print(result)


def cmd_status(gm, args):
    s = gm.get_status()
    print(f"回合: {s['round']} | 阶段: {s['phase_label']}")
    if s["winner"]:
        print(f"胜负: {s['winner_label']} 获胜")
    else:
        print(f"存活: {s['alive_count']}/{s['total_players']} 人 ({', '.join(s['alive_players']) if s['alive_players'] else '无'})")
    if s.get("sheriff"):
        print(f"警长: {s['sheriff']}")
    if s.get("current_step_name"):
        print(f"当前步骤: [{s['current_action']}] {s['current_step_name']} ({s['flow_step']+1}/{s['flow_total']})")
        print(f"提示: {s['current_step_hint']}")
    elif s["phase"] != "ended":
        print(f"步骤: {s['flow_step']}/{s['flow_total']} (全部完成)")
    print(f"日志: {s['log_count']} 条")


def cmd_next(gm, args):
    result = gm.execute_next(args.response)
    print(result)


def main():
    sys.stdout.reconfigure(encoding="utf-8")
    parser = argparse.ArgumentParser(description="半自动GM - 自动化狼人杀游戏流程")
    sub = parser.add_subparsers(dest="command", required=True)

    p_start = sub.add_parser("start", help="开始新游戏")
    p_start.add_argument("players", nargs="+", help="玩家名列表")

    sub.add_parser("run-night", help="自动运行夜间流程")

    sub.add_parser("run-day", help="自动运行白天流程")

    sub.add_parser("run-round", help="自动运行一整轮（夜间+白天）")

    sub.add_parser("status", help="显示当前游戏状态")

    p_next = sub.add_parser("next", help="执行下一步")
    p_next.add_argument("response", nargs="?", default=None, help="玩家的回复内容")

    args = parser.parse_args()
    gm = AutoGameMaster()

    command_map = {
        "start": cmd_start,
        "run-night": cmd_run_night,
        "run-day": cmd_run_day,
        "run-round": cmd_run_round,
        "status": cmd_status,
        "next": cmd_next,
    }

    handler = command_map.get(args.command)
    if handler:
        handler(gm, args)
    else:
        parser.print_help()


if __name__ == "__main__":
    main()
