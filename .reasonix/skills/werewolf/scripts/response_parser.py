#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""response_parser.py — 增强回复解析器，多策略解析AI玩家回复"""

import json, sys
from pathlib import Path
from dataclasses import dataclass, field
from typing import Optional

from roles import load_config
from utils import extract_name, extract_votes_from_responses, extract_witch_action

PROJECT_ROOT = Path(__file__).parent.parent.parent.parent.parent
GAME_FILE = PROJECT_ROOT / "werewolf_state.json"


@dataclass
class ParseResult:
    raw: str
    parsed: dict
    confidence: float
    errors: list[str] = field(default_factory=list)


class ResponseParser:
    """多策略回复解析器"""

    def __init__(self, state_path: Optional[str] = None):
        self.state_path = Path(state_path) if state_path else GAME_FILE
        self.state = self._load_state()

    def _load_state(self) -> dict:
        return json.loads(self.state_path.read_text(encoding="utf-8"))

    def reload(self):
        self.state = self._load_state()

    def _get_alive(self) -> list[str]:
        return [n for n, p in self.state["players"].items() if p["alive"]]

    def _get_all_players(self) -> list[str]:
        return list(self.state["players"].keys())

    def parse_vote(self, text: str) -> ParseResult:
        alive_players = self._get_alive()
        if not text or not text.strip():
            return ParseResult(raw=text, parsed={}, confidence=0.0, errors=["空回复"])
        target = extract_name(text, alive_players)
        if target:
            return ParseResult(raw=text, parsed={"target": target}, confidence=0.95)
        if ":" in text:
            parts = text.split(":", 1)
            voter, rest = parts[0].strip(), parts[1].strip()
            if voter in self.state["players"]:
                target = extract_name(rest, alive_players)
                if target:
                    return ParseResult(raw=text, parsed={"voter": voter, "target": target}, confidence=0.9)
        return ParseResult(raw=text, parsed={}, confidence=0.0, errors=["未识别出投票目标"])

    def parse_wolf_kill(self, texts: list[str]) -> dict:
        alive_players = self._get_alive()
        cfg = load_config()
        if cfg.get("wolf_self_kill"):
            candidates = alive_players
        else:
            candidates = [n for n in alive_players if self.state["players"][n]["role"] not in ("wolf", "mech_wolf")]
        votes = {}
        invalid = []
        for text in texts:
            if not text or ":" not in text:
                invalid.append(text)
                continue
            _, rest = text.split(":", 1)
            target = extract_name(rest.strip(), candidates)
            if target:
                votes[target] = votes.get(target, 0) + 1
            else:
                invalid.append(text)
        result = {
            "votes": votes,
            "invalid": invalid,
            "total": len(texts),
            "valid": len(votes),
        }
        if votes:
            max_v = max(votes.values())
            top = [t for t, v in votes.items() if v == max_v]
            result["tie"] = len(top) > 1
            result["selected"] = top  # 始终返回list，调用者检查 tie 决定如何处理
        return result

    def parse_witch_action(self, text: str) -> tuple[bool, Optional[str]]:
        return extract_witch_action(text, self._get_alive())

    def parse_seer_check(self, text: str) -> Optional[str]:
        return extract_name(text, self._get_alive())

    def parse_guard(self, text: str) -> Optional[str]:
        skip = {"空过", "跳过", "skip", "none", "不守", "空"}
        if text.strip() in skip:
            return None
        return extract_name(text, self._get_alive())

    def parse_sheriff_transfer(self, text: str) -> Optional[str]:
        return extract_name(text, self._get_alive())

    def validate_format(self, text: str, expected: str) -> bool:
        validators = {
            "vote": lambda t: ":" in t and extract_name(t.split(":", 1)[1].strip(), self._get_alive()) is not None,
            "wolf": lambda t: ":" in t and "刀" in t,
            "witch": lambda t: any(w in t for w in ("救", "不救", "skip", "毒")),
            "seer": lambda t: extract_name(t, self._get_alive()) is not None,
            "guard": lambda t: extract_name(t, self._get_alive()) is not None or t.strip() in ("空过", "跳过", "skip", "不守", "空"),
        }
        validator = validators.get(expected)
        if not validator:
            return False
        return validator(text)

    def batch_parse_votes(self, responses: list[str]) -> dict:
        all_players = self._get_all_players()
        pairs = extract_votes_from_responses(responses, all_players)
        result = {"pairs": pairs, "total": len(responses), "valid": len(pairs)}
        tally = {}
        for voter, target in pairs:
            weight = 1.5 if self.state.get("sheriff") == voter else 1.0
            tally[target] = tally.get(target, 0) + weight
        result["tally"] = tally
        if tally:
            max_v = max(tally.values())
            top = [t for t, v in tally.items() if v == max_v]
            result["selected"] = top  # 始终返回list，调用者用 [0] 取首个
        return result


def _parse_result_to_dict(r: ParseResult) -> dict:
    return {"raw": r.raw, "parsed": r.parsed, "confidence": r.confidence, "errors": r.errors}


def cmd_parse(args):
    parser = ResponseParser()
    text = args.text
    action_type = args.type or "vote"
    if action_type == "vote":
        result = _parse_result_to_dict(parser.parse_vote(text))
    elif action_type == "witch":
        save, poison = parser.parse_witch_action(text)
        result = {"save": save, "poison_target": poison}
    elif action_type == "seer":
        result = {"target": parser.parse_seer_check(text)}
    elif action_type == "guard":
        result = {"target": parser.parse_guard(text)}
    elif action_type == "sheriff":
        result = {"target": parser.parse_sheriff_transfer(text)}
    else:
        result = _parse_result_to_dict(parser.parse_vote(text))
    print(json.dumps(result, ensure_ascii=False, indent=2))


def cmd_parse_wolf(args):
    parser = ResponseParser()
    result = parser.parse_wolf_kill(args.texts)
    print(json.dumps(result, ensure_ascii=False, indent=2))


def cmd_parse_votes(args):
    parser = ResponseParser()
    result = parser.batch_parse_votes(args.texts)
    print(json.dumps(result, ensure_ascii=False, indent=2))


def cmd_validate(args):
    parser = ResponseParser()
    ok = parser.validate_format(args.text, args.type)
    print(json.dumps({"valid": ok, "text": args.text, "type": args.type}, ensure_ascii=False, indent=2))


def main():
    import argparse
    sys.stdout.reconfigure(encoding='utf-8')
    main_parser = argparse.ArgumentParser(description="增强回复解析器 - 解析AI玩家回复")
    sub = main_parser.add_subparsers(dest="command", required=True)

    p_parse = sub.add_parser("parse", help="解析单个回复")
    p_parse.add_argument("text", help="要解析的回复文本")
    p_parse.add_argument("--type", choices=["vote", "witch", "seer", "guard", "sheriff"], default="vote", help="回复类型")

    p_wolf = sub.add_parser("parse-wolf", help="解析多个狼人回复")
    p_wolf.add_argument("texts", nargs="+", help="狼人回复列表")

    p_votes = sub.add_parser("parse-votes", help="解析多个投票回复")
    p_votes.add_argument("texts", nargs="+", help="投票回复列表")

    p_val = sub.add_parser("validate", help="验证回复格式")
    p_val.add_argument("text", help="要验证的回复文本")
    p_val.add_argument("type", choices=["vote", "wolf", "witch", "seer", "guard"], help="期望格式类型")

    args = main_parser.parse_args()

    if args.command == "parse":
        cmd_parse(args)
    elif args.command == "parse-wolf":
        cmd_parse_wolf(args)
    elif args.command == "parse-votes":
        cmd_parse_votes(args)
    elif args.command == "validate":
        cmd_validate(args)


if __name__ == "__main__":
    main()
