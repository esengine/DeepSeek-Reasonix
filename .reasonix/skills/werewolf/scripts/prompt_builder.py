#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""prompt_builder.py — 结构化Prompt生成器"""

import json, sys, re
from pathlib import Path

PROJECT_ROOT = Path(__file__).parent.parent.parent.parent.parent
GAME_FILE = PROJECT_ROOT / "werewolf_state.json"
CONFIG_FILE = PROJECT_ROOT / "werewolf_config.json"
PROMPT_DIR = Path(__file__).parent / "prompts"
STRATEGIES_DIR = Path(__file__).parent / "strategies"

# action → 策略文件映射（选段）
STRATEGY_FILE_MAP = {
    "wolf_strategy": ["wolf/core.md", "wolf/hunting/godsniff.md"],
    "wolf_adjust": ["wolf/tactics.md", "wolf/hunting/godsniff.md"],
    "wolf_claim": ["wolf/claim.md"],
    "wolf_hunting": ["wolf/hunting/godsniff.md"],
    "wolf_deep": ["wolf/core.md"],
    "good_day": ["good/general.md"],
    "good_deflect": ["good/deflect.md"],
    "god_hide": ["good/god_hide.md"],
}

# action → 核心策略概念（精简版，必注入）
ACTION_CORE_STRATEGY = {
    "night-wolf": {
        "wolf": [
            "首夜盲刀：无信息，可随机/刀边缘位",
            "后续夜晚：根据白天信息选目标",
            "刀人优先级：女巫(有毒) > 预言家(验人) > 守卫 > 猎人 > 村民",
            "自刀骗药：首夜自刀获银水信任，配合悍跳女巫发假银水",
        ],
        "悍跳狼": [
            "继续悍跳还是转倒钩？看发言差距和票型",
            "如果好人站边你→继续悍跳",
            "如果好人回头→转倒钩做身份",
        ],
        "倒钩狼": [
            "站边真预，用正逻辑做自己好人面",
            "帮真预归票狼队友，活到后期反水",
        ],
        "深水狼": [
            "活到最后，不能被抗推",
            "用正逻辑盘双边，不要上焦点位",
            "发言要像闭眼好人",
        ],
    },
    "night-seer": {
        "seer": [
            "查验优先级：疑似狼 > 位置关键 > 高配玩家",
            "警徽流：2+1（1警上+1警下+1备用）",
            "被查杀时：验人正视角，要求守卫守你",
            "与守卫配合：不要明说，可在警徽流中暗示",
        ],
    },
    "night-witch": {
        "witch": [
            "解药决策：首夜救人性价比高，银水大概率好人",
            "毒药决策：必须满足2条以上才毒（身份明确+无反转+威胁评估）",
            "毒药禁忌：不因'让我毒他'就毒，不因'发言差'就毒",
            "宁可不毒，也不要毒错好人",
        ],
    },
    "night-guard": {
        "guard": [
            "首夜推荐空过，获信息优势",
            "守人优先级：预言家 > 女巫 > 猎人",
            "与女巫配合：避免同守同救",
            "不能连续两晚守同一人",
            "混合策略：随机守/空过，不能被狼读透",
        ],
    },
    "day-speech": {
        "villager": [
            "爆水发言：超高思考量、闭眼视角、找狼视角",
            "敌意差异：对悍跳狼敌意大，对站错边的好人更谨慎",
            "对话vs辩解：好人更愿对话劝回头",
        ],
        "wolf": [
            "模仿闭眼视角，不能聊出睁眼信息",
            "四种打法：冲锋/垫飞/倒钩/深水",
            "表水技巧：不慌不认狼，反打质疑者，点真狼坑",
        ],
        "seer": [
            "报查验+警徽流，安排工作",
            "被质疑时解释而不是反打",
            "对站错边的好人要劝，不要打",
        ],
    },
    "sheriff_withdraw": {
        "seer": "绝对不能退水，必须拿警徽带队",
        "guard": "强烈建议退水，守卫拿警徽=吃首刀",
        "witch": "建议退水，女巫拿警徽容易成为刀口",
        "hunter": "可退水可不退，看局势选择",
        "wolf_悍跳": "绝对不能退水，退水=认狼",
        "wolf_非悍跳": "建议退水恢复投票权",
        "villager": "建议退水让位",
    },
}

ROLE_LABELS = {
    "villager": "村民", "seer": "预言家", "witch": "女巫",
    "hunter": "猎人", "guard": "守卫", "idiot": "白痴",
    "wolf": "狼人", "mech_wolf": "机械狼", "wolf_king": "狼王",
}

WOLF_ROLES = {"wolf", "mech_wolf", "wolf_king"}
GOD_ROLES = {"seer", "witch", "hunter", "guard", "idiot"}

# action → 模板文件名映射
ACTION_TEMPLATE_MAP = {
    "day-speech": "day-speech.md",
    "day-vote": "day-vote.md",
    "day-hunter-active": "day-hunter-active.md",
    "day-idiot-reveal": "day-idiot-reveal.md",
    "night-wolf": "night-wolf.md",
    "night-seer": "night-seer.md",
    "night-witch": "night-witch.md",
    "night-guard": "night-guard.md",
    "night-mimic": "night-mimic.md",
    "sheriff": "sheriff.md",
    "sheriff_withdraw": "sheriff_withdraw.md",
    "last-words": "last-words.md",
    "wolf-explode": "wolf-explode.md",
    "wolf_strategy": "wolf-strategy.md",
    "wolf_adjust": "wolf-adjust.md",
    "wolf_claim": "wolf-claim.md",
    "wolf_hunting": "wolf-hunting.md",
    "wolf_deep": "wolf-deep.md",
    "good_day": "good-day.md",
    "god_hide": "god-hide.md",
}

# action -> expected role (None means any role can use this action)
ACTION_ROLE_MAP = {
    "night-wolf": "wolf",
    "night-seer": "seer",
    "night-witch": "witch",
    "night-guard": "guard",
    "night-mimic": "mech_wolf",
    "day-hunter-active": "hunter",
    "day-idiot-reveal": "idiot",
    "wolf-explode": "wolf",
    "last-words": None,  # any dead player
    "wolf_strategy": "wolf",
    "wolf_adjust": "wolf",
    "wolf_claim": "wolf",
    "wolf_hunting": "wolf",
    "wolf_deep": "wolf",
}


class PromptBuilder:
    """结构化Prompt生成器"""

    def __init__(self, state_path=None, config_path=None):
        self.state_path = Path(state_path) if state_path else GAME_FILE
        self.config_path = Path(config_path) if config_path else CONFIG_FILE
        self.state = self._load_state()
        self.config = self._load_config()

    def _load_state(self):
        return json.loads(self.state_path.read_text(encoding="utf-8"))

    def _load_config(self):
        if self.config_path.exists():
            return json.loads(self.config_path.read_text(encoding="utf-8"))
        return {}

    def reload(self):
        """重新加载状态和配置（在外部修改后调用）。"""
        self.state = self._load_state()
        self.config = self._load_config()

    def list_actions(self):
        """列出所有支持的action类型。"""
        return list(ACTION_TEMPLATE_MAP.keys())

    def load_template(self, action: str) -> str:
        """加载prompt模板。"""
        filename = ACTION_TEMPLATE_MAP.get(action)
        if not filename:
            raise ValueError(f"未知action: {action}，可用: {', '.join(self.list_actions())}")
        path = PROMPT_DIR / filename
        if not path.exists():
            raise FileNotFoundError(f"模板文件不存在: {path}")
        return path.read_text(encoding="utf-8")

    def get_alive_players(self):
        return [n for n, p in self.state["players"].items() if p["alive"]]

    def get_dead_players(self):
        return [n for n, p in self.state["players"].items() if not p["alive"]]

    def get_wolves(self):
        return [n for n, p in self.state["players"].items() if p["alive"] and p["role"] in WOLF_ROLES]

    def get_wolf_teammates(self, player_name):
        """获取狼队友（不含自己）。"""
        return [n for n in self.get_wolves() if n != player_name]

    def get_god_players(self):
        return [n for n, p in self.state["players"].items() if p["alive"] and p["role"] in GOD_ROLES]

    def get_context(self, player_name: str) -> dict:
        """获取玩家的专属上下文。"""
        state = self.state
        players = state["players"]
        if player_name not in players:
            raise ValueError(f"玩家不存在: {player_name}")

        player = players[player_name]
        role = player["role"]
        alive_list = self.get_alive_players()
        dead_list = self.get_dead_players()
        is_alive = player["alive"]
        is_wolf = role in WOLF_ROLES
        log = state.get("log", [])

        # 推导夜间死亡信息
        death_info = "平安夜"
        for e in reversed(log):
            if "昨夜死亡" in e:
                death_info = e
                break
            if "昨夜平安" in e:
                death_info = e
                break

        # 推导对跳预言家信息
        claimed_seer = None
        counter_claimed = None
        seer_checks = []
        for e in log:
            if "跳预言家" in e or "跳预" in e:
                # "深哥跳预言家查杀大克"
                parts = e.replace("跳预言家", "跳预").split("跳预")
                name = parts[0].strip()
                if not claimed_seer:
                    claimed_seer = name
                elif not counter_claimed:
                    counter_claimed = name
            if "查验" in e and "=" in e:
                seer_checks.append(e)

        # 谁被处决
        executed = None
        for e in reversed(log):
            if "处决了" in e:
                executed = e.replace("处决了 ", "").strip()
                break

        # 狼人专属：wolf_plans
        wolf_plan = None
        if is_wolf:
            plans = player.get("wolf_plans", [])
            if plans:
                wolf_plan = plans[-1]

        ctx = {
            "player_name": player_name,
            "role": role,
            "role_label": ROLE_LABELS.get(role, role),
            "alive": is_alive,
            "alive_list": alive_list,
            "alive_str": "、".join(alive_list),
            "dead_list": dead_list,
            "dead_str": "、".join(dead_list),
            "round": state.get("round", 1),
            "phase": state.get("phase", "unknown"),
            "sheriff": state.get("sheriff", None),
            "winner": state.get("winner", None),
            "log": log,
            "player_order": state.get("player_order", alive_list),
            "death_info": death_info,
            "executed": executed or "无",
            "claimed_seer": claimed_seer or "无",
            "counter_claimed": counter_claimed or "无",
            "seer_checks": seer_checks,
            "wolf_plan": wolf_plan,
        }

        if is_alive:
            ctx["is_final_battle"] = self._check_final_battle()

        # 狼人专属信息
        if is_wolf:
            ctx["wolf_teammates"] = self.get_wolf_teammates(player_name)
            ctx["wolf_teammates_str"] = "、".join(ctx["wolf_teammates"])
            ctx["all_wolves"] = self.get_wolves()
            wolf_plans = player.get("wolf_plans", [])
            if wolf_plans:
                latest = wolf_plans[-1]
                ctx["wolf_strategy"] = latest.get("strategy", "")
                ctx["wolf_claim"] = latest.get("claim", "")
                ctx["wolf_check"] = latest.get("check", "")
        else:
            ctx["wolf_teammates"] = []
            ctx["wolf_teammates_str"] = ""

        # 历史信息
        ctx["speeches"] = player.get("speeches", [])
        ctx["votes"] = player.get("votes", [])
        ctx["night_plans"] = player.get("night_plans", [])
        ctx["private_notes"] = player.get("private_notes", [])

        # 角色专属状态
        if role == "witch":
            ctx["witch_antidote_used"] = player.get("witch_antidote_used", False)
            ctx["witch_poison_used"] = player.get("witch_poison_used", False)
            ctx["witch_saved"] = player.get("witch_saved", False)
        if role == "guard":
            ctx["last_guard_target"] = state.get("last_guard_target", None)
        if role == "seer":
            seer_checks = [log for log in state.get("log", []) if "查验" in log]
            ctx["seer_checks"] = seer_checks
        if role == "idiot":
            ctx["idiot_revealed"] = player.get("idiot_revealed", False)
        if role == "hunter":
            ctx["can_hunter_shoot"] = player.get("can_hunter_shoot", True)
        if role == "mech_wolf":
            ctx["mimic_target"] = player.get("mimic_target", None)

        return ctx

    def _check_final_battle(self):
        al = self.get_alive_players()
        wc = len(self.get_wolves())
        gc = len(al) - wc
        if wc >= gc:
            return "critical"
        if wc * 2 >= gc:
            return "tense"
        if len(al) <= 4:
            return "tense"
        return None

    def _get_recent_log(self, n=5):
        log = self.state.get("log", [])
        return log[-n:] if log else []

    def build(self, action: str, player_name: str) -> str:
        """为指定玩家生成完整prompt。"""
        ctx = self.get_context(player_name)
        ctx["action"] = action
        ctx["expected_role"] = ACTION_ROLE_MAP.get(action)
        template = self.load_template(action)

        parts = [
            self._build_header(ctx),
            self._build_role_section(ctx),
            self._build_state_section(ctx),
            self._build_strategy_section(ctx),
        ]

        # 加载并填充模板
        filled = self._fill_template(template, ctx)
        parts.append(filled)

        # 格式要求
        parts.append(self._build_format_section(ctx))

        return "\n\n".join(parts)

    def build_batch(self, action: str) -> dict[str, str]:
        """为所有存活玩家批量生成prompt。"""
        alive_list = self.get_alive_players()
        if not alive_list:
            return {}
        results = {}
        for name in alive_list:
            try:
                results[name] = self.build(action, name)
            except (ValueError, FileNotFoundError) as e:
                results[name] = f"[ERROR] {e}"
        return results

    def _build_header(self, ctx: dict) -> str:
        name = ctx["player_name"]
        round_n = ctx["round"]
        phase = {"night": "夜晚", "day": "白天", "vote": "投票"}.get(ctx["phase"], ctx["phase"])
        status = "存活" if ctx["alive"] else "已死亡"
        
        # Use action's role if available, otherwise player's actual role
        expected_role = ctx.get("expected_role")
        if expected_role:
            role_label = ROLE_LABELS.get(expected_role, expected_role)
        else:
            role_label = ctx["role_label"]
        
        return f"# {name} — {role_label}（第{round_n}{phase}，{status}）"

    def _build_role_section(self, ctx: dict) -> str:
        role_label = ctx["role_label"]
        is_wolf = ctx["role"] in WOLF_ROLES
        expected_role = ctx.get("expected_role")
        
        # If action has an expected role, show that instead of actual role
        if expected_role:
            action_role_label = ROLE_LABELS.get(expected_role, expected_role)
            lines = [f"## 身份信息", f"你的身份：{action_role_label}"]
        else:
            lines = [f"## 身份信息", f"你的身份：{role_label}"]
        
        if ctx["sheriff"] == ctx["player_name"]:
            lines.append("你是警长（投票权重1.5）")
        if is_wolf:
            if ctx["wolf_teammates"]:
                lines.append(f"狼队友：{ctx['wolf_teammates_str']}")
            else:
                lines.append("你是独狼！")
        mimic = ctx.get("mimic_target")
        if mimic:
            lines.append(f"你模仿的身份：{ROLE_LABELS.get(mimic, mimic)}")
        return "\n".join(lines)

    def _build_state_section(self, ctx: dict) -> str:
        lines = ["## 当前局势"]
        lines.append(f"存活（{len(ctx['alive_list'])}人）：{ctx['alive_str']}")
        if ctx["dead_list"]:
            lines.append(f"已死亡：{ctx['dead_str']}")
        if ctx["sheriff"]:
            lines.append(f"警长：{ctx['sheriff']}")

        recent = self._get_recent_log(5)
        if recent:
            lines.append("近期事件：")
            for entry in recent:
                lines.append(f"  · {entry}")

        final = ctx.get("is_final_battle")
        if final == "critical":
            lines.append("[决胜局] 狼人数量≥好人数量，投错即输！")
        elif final == "tense":
            lines.append("[决胜局] 局势紧张！")

        return "\n".join(lines)

    def _build_strategy_section(self, ctx: dict) -> str:
        lines = ["## 历史记录"]
        speeches = ctx.get("speeches", [])
        if speeches:
            lines.append("你的发言记录：")
            for sp in speeches[-5:]:
                lines.append(f"  · {sp}")
        votes = ctx.get("votes", [])
        if votes:
            lines.append(f"你的投票记录：{' → '.join(votes[-5:])}")
        notes = ctx.get("private_notes", [])
        if notes:
            lines.append("你的笔记：")
            for note in notes[-3:]:
                lines.append(f"  · {note}")

        # === 新策略注入系统 ===
        action = ctx.get("action", "")
        role = ctx.get("role", "")
        is_wolf = role in WOLF_ROLES

        # Layer 1: 核心策略概念（必注入，精简版）
        core_strategies = ACTION_CORE_STRATEGY.get(action, {})
        if core_strategies:
            lines.append("")
            lines.append("## 策略参考")
            # 根据角色选择对应策略
            if is_wolf:
                # 狼人：尝试匹配具体角色（悍跳/倒钩/深水）
                wolf_plan = ctx.get("wolf_plan") or {}
                wolf_role = wolf_plan.get("strategy", "")
                matched = False
                for tag in ["悍跳", "倒钩", "深水"]:
                    if tag in wolf_role and f"wolf_{tag}" in core_strategies:
                        for tip in core_strategies[f"wolf_{tag}"]:
                            lines.append(f"· {tip}")
                        matched = True
                        break
                if not matched and "wolf" in core_strategies:
                    for tip in core_strategies["wolf"]:
                        lines.append(f"· {tip}")
            elif role in core_strategies:
                strategy = core_strategies[role]
                if isinstance(strategy, list):
                    for tip in strategy:
                        lines.append(f"· {tip}")
                elif isinstance(strategy, str):
                    lines.append(f"· {strategy}")

        # Layer 2: 情境策略（根据游戏状态选）
        round_n = ctx.get("round", 1)
        alive_count = len(ctx.get("alive_list", []))
        is_final = ctx.get("is_final_battle")

        if is_final:
            lines.append("")
            lines.append("⚠️ 决胜局：狼人数量接近好人，每一票都决定胜负！")
            lines.append("· 必须找到确定的狼人才投票")
            lines.append("· 考虑对手的思考层级（千层饼分析）")
            lines.append("· 保持冷静，不要被情绪左右")

        if round_n >= 3 and alive_count <= 6:
            lines.append("")
            lines.append("⚠️ 后期局：信息有限，谨慎决策")

        # Layer 3: 策略文件选段（高级参考）
        strategy_files = STRATEGY_FILE_MAP.get(action, [])
        if not strategy_files:
            for action_prefix, files in STRATEGY_FILE_MAP.items():
                if action.startswith(action_prefix):
                    strategy_files = files
                    break
        if strategy_files:
            lines.append("")
            lines.append("## 深度参考")
            for sf in strategy_files[:1]:  # 只取第一个文件，控制长度
                path = STRATEGIES_DIR / sf
                if path.exists():
                    text = path.read_text(encoding="utf-8")
                    # 提取关键段落（跳过标题和分隔线）
                    key_lines = []
                    for pl in text.strip().split("\n"):
                        pl = pl.strip()
                        if pl and not pl.startswith("---") and not pl.startswith("#"):
                            key_lines.append(pl)
                        if len(key_lines) >= 30:  # 控制在30行内
                            break
                    lines.append(f"> **{sf}**")
                    for kl in key_lines:
                        lines.append(f"> {kl}")

        return "\n".join(lines)

    def _build_format_section(self, ctx: dict) -> str:
        return "## 输出格式\n直接输出你的决策，不要加前缀。简洁有力，≤50字。"

    def _fill_template(self, template: str, ctx: dict) -> str:
        s = template
        # 基础替换
        s = s.replace("【角色】", ctx["role_label"])
        s = s.replace("【角色名】", ctx["player_name"])
        s = s.replace("【列表】", ctx["alive_str"])
        s = s.replace("【N】", str(ctx["round"]))
        s = s.replace("【名字】", ctx["player_name"])
        s = s.replace("【身份】", ctx["role_label"])
        s = s.replace("【谁死了 / 平安夜】", ctx["death_info"])
        s = s.replace("【信息】", ctx["death_info"])
        s = s.replace("【悍跳狼名字】", ctx["claimed_seer"] if ctx["claimed_seer"] != "无" else "未知")
        s = s.replace("【记录，如 A=好人，B=狼人】", "；".join(ctx["seer_checks"][-3:]) if ctx["seer_checks"] else "暂无")
        s = s.replace("【女巫】", "女巫")
        s = s.replace("【守卫】", "守卫")
        s = s.replace("【猎人】", "猎人")
        s = s.replace("【白痴】", "白痴")
        s = s.replace("【狼人】", "狼人")
        s = s.replace("【机械狼】", "机械狼")
        # 女巫药水状态
        if ctx.get("witch_antidote_used") is not None:
            antidote = "已用" if ctx["witch_antidote_used"] else "可用"
            poison = "已用" if ctx["witch_poison_used"] else "可用"
            s = s.replace("【可用 / 已用】", f"解药({antidote}) 毒药({poison})")
        else:
            s = s.replace("【可用 / 已用】", "可用")
        # 守卫上次目标
        last_guard = ctx.get("last_guard_target", "空")
        s = s.replace("【名字 / 空过】", last_guard if last_guard else "空")
        # 对跳信息：从log提取【A】跳预查杀【B】等
        claimed = ctx.get("claimed_seer", "无")
        counter = ctx.get("counter_claimed", "无")
        check_log = ctx.get("seer_checks", [])
        check_a = "无"
        check_b = "无"
        check_d = "无"
        check_e = "无"
        check_c = "无"
        check_f = "无"
        for c in check_log:
            # "预言家查验 X = Y"
            parts = c.replace("预言家查验 ", "").split(" = ")
            if len(parts) == 2:
                if check_a == "无":
                    check_a = parts[0].strip()
                    check_b = parts[1].strip()
                elif check_d == "无":
                    check_d = parts[0].strip()
                    check_e = parts[1].strip()
        s = s.replace("【A】", claimed if claimed != "无" else "A")
        s = s.replace("【B】", check_b if check_b != "无" else "B")
        s = s.replace("【C】", check_c if check_c != "无" else "C")
        s = s.replace("【D】", counter if counter != "无" else "D")
        s = s.replace("【E】", check_e if check_e != "无" else "E")
        s = s.replace("【F】", check_f if check_f != "无" else "F")
        # 狼人当前角色
        wp = ctx.get("wolf_plan")
        current_role = "狼人"
        if wp:
            strategy = wp.get("strategy", "")
            for tag in ["悍跳", "冲锋", "倒钩", "深水", "垫飞"]:
                if tag in strategy:
                    current_role = tag + "狼"
                    break
        s = s.replace("【当前角色】", current_role)
        s = s.replace("【悍跳/真预】", "悍跳" if ctx["role"] in WOLF_ROLES else "真预")
        s = s.replace("【真预/悍跳】", "真预" if ctx["role"] not in WOLF_ROLES else "悍跳")
        # 是否条件
        s = s.replace("【是/否】", "是")
        # 对跳信息（退水prompt专用）
        claimed_seer = ctx.get("claimed_seer", "无")
        counter_claimed = ctx.get("counter_claimed", "无")
        if claimed_seer != "无" and counter_claimed != "无":
            s = s.replace("【对跳信息】", f"场上对跳：{claimed_seer} vs {counter_claimed}")
        elif claimed_seer != "无":
            s = s.replace("【对跳信息】", f"场上跳预言家：{claimed_seer}")
        else:
            s = s.replace("【对跳信息】", "暂无对跳信息")
        # 策略提示（退水prompt专用）
        s = s.replace("【策略提示】", "")
        # 替换剩余【】占位符为"未知"
        remaining = re.findall(r'【[^】]*】', s)
        for ph in remaining:
            s = s.replace(ph, f"[{ph[1:-1]}]" if ph[1:-1] else "")
        return s

    def to_parallel_format(self, action: str) -> str:
        """生成可直接粘贴进parallel_tasks的格式。"""
        prompts = self.build_batch(action)
        if not prompts:
            return "# 无存活玩家"
        lines = []
        for name, prompt in prompts.items():
            escaped = prompt.replace("\\", "\\\\").replace('"', '\\"')
            lines.append(f'"{name}": "{escaped}"')
        return "{\n  " + ",\n  ".join(lines) + "\n}"


def main():
    import argparse
    sys.stdout.reconfigure(encoding='utf-8')
    parser = argparse.ArgumentParser(description="结构化Prompt生成器")
    sub = parser.add_subparsers(dest="command", required=True)

    p_build = sub.add_parser("build", help="为指定玩家生成prompt")
    p_build.add_argument("action", help="action类型")
    p_build.add_argument("player", help="玩家名")

    p_batch = sub.add_parser("build-batch", help="为所有存活玩家批量生成prompt")
    p_batch.add_argument("action", help="action类型")
    p_batch.add_argument("--players", nargs="*", help="指定玩家列表（默认所有存活）")
    p_batch.add_argument("--json", action="store_true", help="以JSON格式输出")

    p_list = sub.add_parser("list-actions", help="列出所有支持的action类型")

    p_par = sub.add_parser("to-parallel", help="生成parallel_tasks格式")
    p_par.add_argument("action", help="action类型")

    args = parser.parse_args()
    builder = PromptBuilder()

    if args.command == "list-actions":
        for a in builder.list_actions():
            print(a)
        return

    if args.command == "build":
        try:
            prompt = builder.build(args.action, args.player)
            print(prompt)
        except (ValueError, FileNotFoundError) as e:
            print(f"[ERROR] {e}", file=sys.stderr)
            sys.exit(1)
        return

    if args.command == "build-batch":
        results = builder.build_batch(args.action)
        if args.players:
            results = {n: results.get(n, "") for n in args.players if n in results}
        if args.json:
            print(json.dumps(results, ensure_ascii=False, indent=2))
        else:
            for name, prompt in results.items():
                print(f"===== {name} =====")
                print(prompt)
                print()
        return

    if args.command == "to-parallel":
        output = builder.to_parallel_format(args.action)
        print(output)
        return


if __name__ == "__main__":
    main()
