#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""special_roles.py — 机械狼/模仿狼特殊角色逻辑"""


def apply_mimic_target(s, mimic_role_target):
    """设置机械狼的模仿目标。返回 True 如果有机械狼被设置了目标。"""
    if not mimic_role_target:
        return False
    valid = ["seer", "witch", "hunter", "guard", "villager", "wolf"]
    if mimic_role_target not in valid:
        return False
    for n, p in s["players"].items():
        if p["role"] == "mech_wolf" and p["alive"]:
            p["mimic_target"] = mimic_role_target
            return True
    return False


def get_mimic_seer_result(role):
    """模仿狼被预言家查验时返回的假身份。"""
    from roles import ROLES
    if role == "mech_wolf":
        return ROLES.get("villager", "村民")
    return ROLES.get(role, role)


def get_mimic_role_for_seer(s, player_name):
    """预言家查验模仿狼时看到的身份。"""
    from roles import ROLES
    for n, p in s["players"].items():
        if n == player_name and p["role"] == "mech_wolf" and p.get("mimic_target"):
            return ROLES.get(p["mimic_target"], "村民")
    return None


def has_mimic_antidote_left(s, player_name):
    """模仿狼（模拟女巫）的解药是否已用。"""
    return not s["players"].get(player_name, {}).get("mimic_antidote_used", False)


def has_mimic_poison_left(s, player_name):
    """模仿狼（模拟女巫）的毒药是否已用。"""
    return not s["players"].get(player_name, {}).get("mimic_poison_used", False)


def mark_mimic_antidote_used(s, player_name):
    s["players"][player_name]["mimic_antidote_used"] = True


def mark_mimic_poison_used(s, player_name):
    s["players"][player_name]["mimic_poison_used"] = True
