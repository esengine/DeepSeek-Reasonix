"""night.py — cmd_night, cmd_night_auto"""

import argparse

from state import load_game, save_game, log_event, record_game_result, LOG_FILE, STATS_FILE
from roles import (
    ROLES, ROLE_ICONS, DEFAULT_CONFIG, load_config,
    alive, wolves, find_player, is_wolf_role, pick_roles, is_final_battle,
)
from utils import extract_name, extract_votes_from_responses, extract_witch_action
from special_roles import apply_mimic_target
from .base import process_night_death, GAME_FILE, CONFIG_FILE
from game_flow import validate_night_steps, mark_step_completed


def cmd_night(args):
    cfg = load_config()
    s = load_game()
    s["phase"] = "night"

    witch = find_player(s, "witch")
    if witch and args.kill:
        witch_name = witch[0]
        witch_antidote = not s["players"][witch_name].get("witch_antidote_used", False)
        witch_poison = not s["players"][witch_name].get("witch_poison_used", False)
        if args.kill == witch_name and (witch_antidote or witch_poison):
            if not args.save and not args.poison and not getattr(args, 'no_save', False):
                print(f"[!] 女巫({witch_name})被刀了！必须先问是否自救/用毒")
                print(f"[!] 正确: task问女巫→得回复→night --kill {witch_name} [--save] [--no-save] [--poison X]")
                return
        if witch_antidote and args.kill != witch_name and not args.save and not args.no_save:
            print(f"[!] 女巫({witch_name})有解药，必须先task问女巫是否救{args.kill}")
            print(f"[!] 正确: task问女巫→得回复→night --kill {args.kill} --save")
            return

    # 只在首轮夜间重置状态，避免重复调用清空数据
    if not s.get("_night_acted"):
        for p in s["players"].values():
            p["witch_saved"] = False
            p["guard_protected"] = False
            p["witch_acted_tonight"] = False
        s["_night_acted"] = True

    guard = find_player(s, "guard")
    if guard and args.guard:
        t = args.guard
        if t not in s["players"] or not s["players"][t]["alive"]:
            print(f"[G] 不能守护已死亡玩家，跳过")
        elif t == s.get("last_guard_target"):
            print(f"[G] 守卫不能连续两晚守同一人，跳过")
        else:
            print(f"[G] 守卫 {guard[0]} 守护了 {t}")
            s["players"][t]["guard_protected"] = True
            s["last_guard_target"] = t

    mimic_guard = None
    if cfg.get("mimic_wolf"):
        for n, p in s["players"].items():
            if p["role"] == "mech_wolf" and p["alive"] and p.get("mimic_target") == "guard":
                mimic_guard = n
                break
    if mimic_guard and args.mimic_action:
        t = args.mimic_action
        if t in alive(s):
            print(f"[Mw] 模仿狼(守卫) {mimic_guard} 守护了 {t}")
            s["players"][t]["guard_protected"] = True

    wl = wolves(s, cfg)
    if wl and args.kill:
        t = args.kill
        print(f"[W] 狼人 {', '.join(wl)} 决定刀 {t}")
        s["log"].append(f"狼人刀了 {t}")
        log_event("wolf_kill", {"round": s["round"], "target": t, "wolves": wl})

    witch = find_player(s, "witch")
    killed = args.kill
    if witch:
        if s["players"][witch[0]].get("witch_acted_tonight"):
            if args.save or args.poison:
                print(f"[!] 女巫本晚已行动，跳过")
        elif killed and args.save:
            can_save = True
            if killed in witch:
                if not (cfg.get("witch_self_save_n1") and s["round"] == 1):
                    can_save = False
                    print(f"[!] 女巫不能自救（首夜除外），跳过")
            if can_save and not s["players"][witch[0]].get("witch_antidote_used"):
                print(f"[Wi] 女巫救了 {killed}")
                s["log"].append(f"女巫救了 {killed}")
                log_event("witch_save", {"round": s["round"], "target": killed})
                s["players"][killed]["witch_saved"] = True
                s["players"][witch[0]]["witch_antidote_used"] = True
                s["players"][witch[0]]["witch_acted_tonight"] = True
            elif can_save:
                print(f"[!] 女巫解药已用完，跳过")
        elif killed and not args.save and not getattr(args, 'no_save', False):
            if not s["players"][witch[0]].get("witch_antidote_used"):
                s["players"][witch[0]]["witch_antidote_used"] = True
                print(f"[Wi] 女巫选择不救 {killed}")
                s["log"].append(f"女巫不救 {killed}")
        if args.poison and args.poison in s["players"] and not s["players"][witch[0]].get("witch_poison_used") and not s["players"][witch[0]].get("witch_acted_tonight"):
            print(f"[Wi] 女巫毒了 {args.poison}")
            s["log"].append(f"女巫毒了 {args.poison}")
            log_event("witch_poison", {"round": s["round"], "target": args.poison})
            s["players"][args.poison]["poisoned"] = True
            s["players"][witch[0]]["witch_poison_used"] = True
            s["players"][witch[0]]["witch_acted_tonight"] = True
        elif args.poison and s["players"][witch[0]].get("witch_poison_used"):
            print(f"[!] 女巫毒药已用完，跳过")

    mimic_witch = None
    if cfg.get("mimic_wolf"):
        for n, p in s["players"].items():
            if p["role"] == "mech_wolf" and p["alive"] and p.get("mimic_target") == "witch":
                mimic_witch = n
                break
    if mimic_witch and args.mimic_witch:
        p = s["players"][mimic_witch]
        if p.get("witch_acted_tonight"):
            print(f"[!] 模仿狼(女巫)本晚已行动，跳过")
        else:
            resp = args.mimic_witch
            if killed and "救" in resp and not p.get("mimic_antidote_used"):
                print(f"[Mw] 模仿狼(女巫) {mimic_witch} 救了 {killed}")
                s["players"][killed]["witch_saved"] = True
                p["mimic_antidote_used"] = True
                p["witch_acted_tonight"] = True
            if "毒" in resp:
                poison_t = extract_name(resp, alive(s))
                if poison_t and poison_t in s["players"] and not p.get("mimic_poison_used"):
                    print(f"[Mw] 模仿狼(女巫) {mimic_witch} 毒了 {poison_t}")
                    s["players"][poison_t]["poisoned"] = True
                    p["mimic_poison_used"] = True
                    p["witch_acted_tonight"] = True

    dead = set()
    if args.kill:
        process_night_death(s, "night", args.kill, dead, cfg.get("guard_witch_overlap_lethal", False))
    for n, p in s["players"].items():
        if p.get("poisoned") and p["alive"]:
            dead.add(n)
            if p["role"] == "hunter":
                p["can_hunter_shoot"] = False

    if args.extra_kill and s.get("extra_night_kill") and args.extra_kill in s["players"]:
        if args.extra_kill == killed:
            print(f"[!] 双刀目标与狼刀重复，跳过")
        else:
            t = args.extra_kill
            print(f"[X] 双刀！额外击杀了 {t}")
            s["log"].append(f"额外击杀了 {t}")
            s["players"][t]["alive"] = False
            s["extra_night_kill"] = False

    for n in dead:
        s["players"][n]["alive"] = False
    prev_dead = set(args._prev_dead) if hasattr(args, '_prev_dead') else set()
    all_now = {n for n, p in s["players"].items() if not p["alive"]}
    new_dead = sorted(all_now - prev_dead)
    if new_dead:
        if cfg.get("reveal_role_on_death"):
            roles_str = ", ".join(f"{n}({ROLES[s['players'][n]['role']]})" for n in new_dead)
            print(f"[X] 昨夜死亡: {roles_str}")
        else:
            print(f"[X] 昨夜死亡: {', '.join(new_dead)}")
        s["log"].append(f"昨夜死亡: {', '.join(new_dead)}")
        log_event("night_death", {"round": s["round"], "dead": new_dead})
        if args.last_words and (s["round"] == 1 or cfg.get("night_kill_last_words")):
            for lw in args.last_words:
                if ":" in lw:
                    name, text = lw.split(":", 1)
                    if name in new_dead:
                        s["players"][name]["last_words"] = text
                        print(f"  💬 遗言 [{name}]: {text}")
    else:
        print(f"[M] 昨夜平安")
        s["log"].append("昨夜平安")

    seers = [n for n, p in s["players"].items() if p["role"] == "seer" and p["alive"]]
    if cfg.get("mimic_wolf"):
        for n, p in s["players"].items():
            if p["role"] == "mech_wolf" and p["alive"] and p.get("mimic_target") == "seer":
                seers.append(n)
    for seer in seers:
        if seer not in s["players"]:
            continue
        is_real_seer = s["players"][seer]["role"] == "seer"
        target = args.check if is_real_seer else None
        if not is_real_seer and args.mimic_action:
            target = extract_name(args.mimic_action, list(s["players"].keys()))
            if not target:
                target = args.mimic_action
        if target and target in s["players"] and s["players"][target]["alive"]:
            role = s["players"][target]["role"]
            if role == "mech_wolf" and cfg.get("mimic_wolf"):
                shown = "villager"
            else:
                shown = role
            print(f"[S] {seer} 查验 {target} = {ROLES.get(shown, shown)}")
            s["log"].append(f"预言家查验 {target} = {shown}")
            log_event("seer_check", {"round": s["round"], "seer": seer, "target": target, "result": shown})

    sheriff = s.get("sheriff")
    if sheriff and not s["players"].get(sheriff, {}).get("alive", True):
        if not args.pass_sheriff:
            print(f"[!] 警长 {sheriff} 死亡！必须task问警长传给谁")
            print(f"[!] 所有夜间行动已保存。重新执行时加 --pass-sheriff Y")
            if not args.no_sheriff_confirm:
                save_game(s)  # 先保存夜间结果，不掉数据
                return
        elif args.pass_sheriff and args.pass_sheriff in alive(s):
            s["sheriff"] = args.pass_sheriff
            print(f"[警徽] 前警长 {sheriff} 将警徽传给 {args.pass_sheriff}")
            s["log"].append(f"警徽传给 {args.pass_sheriff}")
        elif args.pass_sheriff:
            print(f"[!] 警长传位失败：{args.pass_sheriff} 已死亡")
            s["sheriff"] = None
        else:
            s["sheriff"] = None
            print(f"[警徽] 警长 {sheriff} 死亡，警徽流失")

    # 猎人开枪：兼容新死者(正常流程)和已死亡未开枪者(补枪)
    if args.hunter_target and args.hunter_target in alive(s):
        shot_hunter = None
        for n, p in s["players"].items():
            if (not p["alive"] and p["role"] == "hunter"
                    and p.get("can_hunter_shoot", True)):
                shot_hunter = n
                break
        if shot_hunter:
            s["players"][args.hunter_target]["alive"] = False
            s["players"][shot_hunter]["can_hunter_shoot"] = False
            print(f"[H] 猎人 {shot_hunter} 开枪带走了 {args.hunter_target}")
            s["log"].append(f"猎人带走了 {args.hunter_target}")
        elif not args.no_shot_warn:
            print(f"[!] 没有可开枪的猎人，跳过")

    s["phase"] = "day"
    s.pop("_night_acted", None)  # 清除夜间标记，允许下一轮重置
    is_final, final_level = is_final_battle(s, cfg)
    if is_final:
        if final_level == "critical":
            print(f"[!] 🔴 决胜局！狼人数量 ≥ 好人数量，好人投错就输！")
        else:
            print(f"[!] ⚠️ 决胜局！狼人数量接近好人，局势紧张！")
    save_game(s)


def cmd_night_auto(args):
    cfg = load_config()
    s = load_game()
    al = alive(s)
    wl = wolves(s, cfg)

    # 验证夜间必需步骤是否已完成 — 暂跳过
    issues = []

    kill_target = None
    if wl and args.wolf:
        if cfg.get("wolf_self_kill"):
            candidates = al
        else:
            candidates = [n for n in al if not is_wolf_role(s["players"][n]["role"])]
        votes = {}
        invalid_wolves = []
        for i, resp in enumerate(args.wolf):
            t = extract_name(resp, candidates)
            if t:
                votes[t] = votes.get(t, 0) + 1
            else:
                if i < len(wl):
                    invalid_wolves.append(wl[i])

        if invalid_wolves:
            print(f"[!] ⚠️ 警告: {len(invalid_wolves)}/{len(wl)} 匹狼回复未识别到目标: {', '.join(invalid_wolves)}")
            print(f"[!] 建议: task要求这些狼人重新回复，格式: 刀:【名字】 理由:...")

        if votes:
            max_votes = max(votes.values())
            top_targets = [t for t, v in votes.items() if v == max_votes]
            kill_target = top_targets[0]
            if len(top_targets) > 1:
                print(f"[W] 狼人投票平票: {', '.join(top_targets)}，取第一个: {kill_target}")
            else:
                print(f"[W] 狼人多数票决定刀 {kill_target} ({max_votes}票)")
        else:
            print(f"[!] ❌ 错误: 所有狼人回复均未识别到刀人目标")
            print(f"[!] 请重新收集狼人回复后重试")
            print(f"[!] 正确格式: --wolf '狼1:刀目标' '狼2:刀目标'")
            print(f"[!] 或手动指定: night --kill 目标名字")
            return

    if kill_target and s["round"] > 1 and s["players"][kill_target]["role"] in ("villager",):
        gods_alive = [n for n in al if s["players"][n]["role"] in ("seer","witch","hunter")]
        if gods_alive:
            print(f"[!] 警告：目标 {kill_target}(村民)不是神职，存活神职: {', '.join(gods_alive)}")
            print(f"[!] 建议重新考虑刀人目标")

    witch = find_player(s, "witch")
    if witch and kill_target:
        wn = witch[0]; wp = s["players"][wn]
        has_antidote = not wp.get("witch_antidote_used", False)
        witch_responded = bool(args.witch)
        if has_antidote and not getattr(args, 'no_save', False) and not witch_responded:
            if kill_target == wn and cfg.get("witch_self_save_n1"):
                print(f"[!] 女巫(你)被刀了！必须先问你是否自救")
                print(f"[!] 正确: task问女巫→night-auto --witch '救'")
                return
            elif kill_target != wn:
                print(f"[!] 女巫({wn})有解药，必须先问是否救{kill_target}")
                print(f"[!] 正确: task问女巫→night-auto --witch '救/跳过/不救'")
                return

    # 参数格式清洗：兼容 "玩家:回复" 和 "回复" 两种格式
    def _strip_prefix(text):
        if text and ':' in text:
            _, _, rest = text.partition(':')
            return rest.strip()
        return text

    guard_target = None
    if args.guard:
        raw = _strip_prefix(args.guard)
        skip = {"空过", "跳过", "skip", "none", "不守", "空"}
        if raw.strip() not in skip:
            t = extract_name(raw, al)
            if t:
                guard_target = t

    check_target = None
    if args.seer:
        raw = _strip_prefix(args.seer)
        t = extract_name(raw, al)
        if t:
            check_target = t

    save, poison_target = extract_witch_action(_strip_prefix(args.witch) if args.witch else "", al)

    hunter_target = None
    if args.hunter:
        raw = _strip_prefix(args.hunter)
        skip = {"不", "没", "skip", "none", "空", "不开枪"}
        if not any(w in raw for w in skip):
            t = extract_name(raw, al)
            if t:
                hunter_target = t

    mimic_role_target = args.mimic_role
    if mimic_role_target and s["round"] == 1:
        valid = ["seer", "witch", "hunter", "guard", "villager", "wolf"]
        if mimic_role_target in valid:
            for n, p in s["players"].items():
                if p["role"] == "mech_wolf" and p["alive"]:
                    p["mimic_target"] = mimic_role_target
                    print(f"[Mw] 机械狼 模仿了 {ROLES.get(mimic_role_target, mimic_role_target)}")
                    s["mimic_wolf_target"] = mimic_role_target
                    break
            save_game(s)

    extra_kill = None
    if s.get("extra_night_kill") and args.extra_kill:
        # 确保机械狼仍然存活
        mech = find_player(s, "mech_wolf") or find_player(s, "wolf")
        if not mech or not s["players"][mech[0]].get("alive"):
            s["extra_night_kill"] = False
            print(f"[!] 双刀目标取消：机械狼已死亡")
        else:
            t = extract_name(args.extra_kill, al)
            if t:
                extra_kill = t

    ns = argparse.Namespace(
        kill=kill_target,
        guard=guard_target,
        check=check_target,
        save=save,
        poison=poison_target,
        extra_kill=extra_kill,
        hunter_target=hunter_target,
        no_save=not save,
        mimic_action=args.mimic_action,
        mimic_witch=args.mimic_witch,
        pass_sheriff=args.pass_sheriff,
        no_sheriff_confirm=getattr(args, 'no_sheriff_confirm', False),
        no_shot_warn=getattr(args, 'no_shot_warn', False),
        last_words=args.last_words,
        _prev_dead=[n for n, p in s["players"].items() if not p["alive"]],
    )
    if args.dry_run or args.collect:
        print(f"[DRY-RUN] 狼人刀: {kill_target} | 守卫: {guard_target} | 查: {check_target} | 女巫: {'救' if save else ''}{'毒'+poison_target if poison_target else ''}{'跳过' if not save and not poison_target else ''}")
        if hunter_target: print(f"  猎人开枪: {hunter_target}")
        if extra_kill: print(f"  双刀: {extra_kill}")
        return
    cmd_night(ns)
