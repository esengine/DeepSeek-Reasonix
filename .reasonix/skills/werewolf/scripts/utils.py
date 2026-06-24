#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""utils.py — 狼人杀文本解析工具函数"""

import re


def _normalize(text):
    """移除Markdown标记和标点符号。"""
    return re.sub(r'[*_~>`#()""\'!.?,。！？、；：]', '', text)


def extract_name(text, candidates, skip_prefix=None):
    """从 AI 回复中提取最可能的玩家名。
    
    优先级：粗体 > 动词前缀 > 最后一次出现的位置 > 最长匹配 > 标准化文本匹配。
    支持模式：投X、我投X、出X、刀X、查X、守X、毒X、救X
    
    skip_prefix: 跳过文本开头的这个名字（用于parallel_tasks中"投票人:回复"格式，
                 避免从投票人名字中误匹配）
    """
    if not text or not candidates:
        return None
    sc = sorted(candidates, key=lambda x: -len(x))
    
    # 如果有skip_prefix，先去掉文本开头的前缀部分
    search_text = text
    if skip_prefix and text.startswith(skip_prefix):
        # 找到第一个冒号后的内容作为搜索目标
        colon_idx = text.find(':')
        if colon_idx != -1:
            search_text = text[colon_idx + 1:]
        else:
            # 无冒号，跳过skip_prefix长度的字符
            search_text = text[len(skip_prefix):]
    
    # 1. 粗体优先
    for c in sc:
        if f'**{c}**' in search_text:
            return c
    # 2. 动词前缀模式：投X、我投X、出X、刀X、查X、守X、毒X、救X
    action_prefixes = ['投', '我投', '出', '刀', '查', '守', '毒', '救', '验', '杀', '带走']
    for prefix in action_prefixes:
        for c in sc:
            if f'{prefix}{c}' in search_text:
                return c
    # 3. 最后出现的位置
    pm = {}
    for c in sc:
        i = 0
        while True:
            i = search_text.find(c, i)
            if i == -1:
                break
            if i not in pm or len(c) > len(pm[i]):
                pm[i] = c
            i += 1
    if pm:
        best = pm[max(pm.keys())]
        return best
    # 4. 标准化文本匹配
    nt = _normalize(search_text)
    for c in sc:
        if _normalize(c) in nt:
            return c
    return None


def extract_votes_from_responses(responses, all_players):
    """从 parallel_tasks 回复中批量解析投票。
    
    支持格式：
    - "投票人:目标" (标准格式)
    - "投票人:投目标"
    - "投票人:我投目标"
    - "投票人:出目标"
    - "投票人:我觉得应该出目标"
    
    返回 [(投票人, 目标), ...]
    """
    if not responses:
        return []
    pairs = []
    for resp in responses:
        if not resp or ":" not in resp:
            continue
        voter, rest = resp.split(":", 1)
        voter = voter.strip()
        if voter not in all_players:
            continue
        # 使用skip_prefix避免从投票人名字中误匹配
        target = extract_name(rest.strip(), all_players, skip_prefix=voter)
        if target:
            pairs.append((voter, target))
    return pairs


def extract_witch_action(text, alive_names):
    """从女巫回复中提取救/毒决策。
    
    返回 (save: bool, poison_target: str|None)
    """
    save = False
    poison_target = None
    if not text:
        return save, poison_target
    low = text.lower()
    # 否定词优先
    if any(w in low for w in ("不救", "能不能救", "怎么救", "skip", "no", "不能救", "没法救", "救不了", "不救")):
        save = False
    elif any(w in low for w in ("救", "save")):
        save = True
    if any(w in low for w in ("不毒", "能不能毒", "no poison", "不能毒", "没法毒")):
        poison_target = None
    else:
        # 规范化冒号：毒:名字、毒：名字 → 统一为毒名字
        normalized = re.sub(r'毒[:：]\s*', '毒', text)
        for name in alive_names:
            if f"毒{name}" in normalized or f"毒 {name}" in normalized:
                poison_target = name
                break
    return save, poison_target
