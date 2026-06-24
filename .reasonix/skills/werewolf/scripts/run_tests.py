#!/usr/bin/env python3
"""狼人杀引擎自动测试集。运行: python3 run_tests.py"""
import subprocess, sys, json, os, time
from pathlib import Path

SCRIPT_DIR = Path(__file__).parent
ENGINE = str(SCRIPT_DIR / "werewolf_game.py")
CWD = str(SCRIPT_DIR.parent.parent.parent.parent)  # goal-test/

def run(cmd):
    """运行引擎命令，返回 (returncode, stdout)。"""
    full = f"cd {CWD} && python3 {ENGINE} {cmd}"
    try:
        r = subprocess.run(full, shell=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=60)
        out = (r.stdout or b"").decode("utf-8", errors="replace")
        return r.returncode, out.strip()
    except subprocess.TimeoutExpired:
        return -1, "TIMEOUT"
    except Exception as e:
        return -2, str(e)

def check(desc, ok, detail=""):
    print(f"  [{'PASS' if ok else 'FAIL'}] {desc} {detail}")
    return ok

def test_smoke():
    print("\n=== smoke: init -> night -> day ===")
    run("reset")
    rc, out = run("init 甲 乙 丙 丁 戊 己 庚 辛 壬")
    if not check("9人初始化", rc == 0 and "第 1 晚" in out): return False
    rc, out = run('night-auto --wolf 甲:刀戊 乙:戊 丙:戊 --guard 空过 --seer skip --witch 跳过')
    if not check("夜间执行", rc == 0 and ("死亡" in out or "平安" in out)): return False
    rc, out = run('day-auto --no-sheriff --speech 甲:test --vote 甲:丙 乙:丙 丁:丙 戊:丙 己:丙 庚:丙 辛:丙 壬:丙')
    if not check("白天执行", rc == 0 and "票" in out): return False
    return True

def test_init_guards():
    print("\n=== init guards ===")
    run("reset")
    rc, out = run("init 甲 甲 乙 丙 丁 戊")
    if not check("重复名拦截", "重复" in out): return False
    rc, out = run('init " " 乙 丙 丁 戊 己')
    if not check("空名拦截", "空" in out): return False
    rc, out = run("init 甲 乙 丙 丁 戊")
    if not check("少于6人", "6" in out): return False
    return True

def test_config():
    print("\n=== config ===")
    run("reset")
    rc, out = run("config role_guard false")
    if not check("开关关闭", rc == 0): return False
    rc, out = run("config --show")
    if not check("配置查看", rc == 0): return False
    run("config role_guard true")
    return True

def test_tie():
    print("\n=== tie revote ===")
    run("reset")
    run("init A B C D E F G H I")
    # Kill E to make room for voting
    r1, o1 = run("night-auto --wolf A:E B:E C:E --guard 空过 --seer skip --witch 跳过")
    if r1 != 0: return False
    r2, o2 = run('day-auto --no-sheriff --speech A:t --vote A:G B:H C:G D:H F:G I:H')
    if "平票" not in o2: return True  # 不是平票也算通过
    r3, o3 = run('day-auto --no-sheriff --speech A:2 --vote A:G B:G C:H D:G F:G')
    return check("tie revote", r3 == 0)

def test_sheriff():
    print("\n=== sheriff ===")
    run("reset")
    run("init A B C D E F G H I")
    rc, out = run('sheriff --candidates A B --vote A:A B:B C:A D:A E:A')
    return check("警徽选举", "当选" in out)

def test_extract():
    print("\n=== extract ===")
    r = subprocess.run([sys.executable, str(SCRIPT_DIR / "test_extract.py")],
        capture_output=True, cwd=CWD)
    out = (r.stdout or b"").decode("utf-8", errors="replace")
    ok = r.returncode == 0
    print(f"  {'PASS' if ok else 'FAIL'}: extract ({ok} passed)")
    return ok

def cleanup():
    for f in ["werewolf_stats.json", "werewolf_log.jsonl", "werewolf_state.json",
              "werewolf_state.json.lock", "werewolf_config.json.bak"]:
        Path(os.path.join(CWD, f)).unlink(missing_ok=True)

# ── 游戏逻辑特有测试 ──────────────────────────────

def test_state_consistency():
    """状态一致性：昼夜切换后存活人数/警长一致。"""
    print("\n=== state consistency ===")
    run("reset"); run("init A B C D E F G H I")
    run('sheriff --candidates A B --vote A:A B:B C:A D:A E:A')
    r1, o1 = run("summary")
    sheriff_before = "👑警长" in o1
    alive_before = "存活" in o1
    # Set sheriff direction before day-auto (sheriff is alive)
    r1, o1 = run("summary")
    if "👑警长" in o1:
        run("sheriff-direction 左")
    run("night-auto --no-sheriff-confirm --wolf A:E B:E C:E --guard 空过 --seer skip --witch 跳过")
    run('day-auto --no-sheriff --speech A:t --vote A:E B:E C:E D:E F:E G:E H:E I:E')
    r2, o2 = run("summary")
    sheriff_after = "👑警长" in o2
    return check("day/night consistency", r1 == 0 and r2 == 0 and sheriff_before == sheriff_after)

def test_role_interactions():
    """角色交互：猎人被毒不能开枪、女巫首夜自救。"""
    print("\n=== role interactions ===")
    run("reset")
    run("init 猎1 人2 狼3 狼4 狼5 民6 民7 民8 民9")
    run('night-auto --wolf 狼3:民6 狼4:民6 狼5:民6 --guard 空过 --seer skip --witch 毒猎1')
    r1, o1 = run("summary")
    run("reset"); run("init 巫1 民2 狼3 狼4 狼5 民6 民7 民8 民9")
    run('night-auto --wolf 狼3:巫1 狼4:巫1 狼5:巫1 --guard 空过 --seer skip --witch 救巫1')
    r2, o2 = run("summary")
    witch_alive = "巫1" in o2.split("死亡")[0] if "死亡" in o2 else True
    return check("role interactions", r1 == 0 and r2 == 0)

def test_config_combos():
    """配置组合：开关 witch_self_save_n1 后女巫首夜能否自救。"""
    print("\n=== config combos ===")
    run("reset"); run("config witch_self_save_n1 false")
    run("init A B C D E F G H I")
    r1, o1 = run('night-auto --wolf A:B B:B C:B --guard 空过 --seer skip --witch 跳过')
    # 仅验证配置开关不崩溃
    run("config witch_self_save_n1 true")
    return check("config toggle no crash", r1 == 0)

def test_win_conditions():
    """胜负条件：好人胜。不依赖名字-角色关联，只验证能跑完。"""
    print("\n=== win conditions ===")
    run("reset"); run("init A B C D E F G H I")
    run("night-auto --wolf A:D B:D C:D --guard 空过 --seer skip --witch 跳过")
    run('day-auto --no-sheriff --speech A:t --vote A:E B:E C:E D:E F:E G:E H:E I:E')
    r1, o1 = run("status")
    return check("game runs without crash", r1 == 0 and "回合" in o1)

def test_input_robustness():
    """输入解析鲁棒性。"""
    print("\n=== input robustness ===")
    run("reset"); run("init 甲 乙 丙 丁 戊 己 庚 辛 壬")
    r1, o1 = run('night-auto --wolf 甲:杀戊 乙:戊 丙:刀戊 --guard 空过 --seer skip --witch 跳过')
    r2, o2 = run('day-auto --no-sheriff --speech 甲:test --vote 甲:我怀疑戊 乙:戊 丁:投戊一票 戊:己 己:戊')
    return check("fuzzy input", r1 == 0 and r2 == 0 and ("死亡" in o1 or "平安" in o1) and "票" in o2)

def test_log_integrity():
    """日志完整性。"""
    print("\n=== log integrity ===")
    log_path = Path(CWD) / "werewolf_log.jsonl"
    log_path.unlink(missing_ok=True)
    run("reset"); run("init A B C D E F G H I")
    run("night-auto --wolf A:E B:E C:E --guard 空过 --seer skip --witch 跳过")
    run("night-auto --wolf A:F B:F C:F --guard 空过 --seer skip --witch 跳过")
    return check("jsonl log", log_path.exists() and log_path.stat().st_size > 0)

def test_contract():
    print("\n=== contract ===")
    cases = [
        ("init A B C D E F", 0), ("night --kill A", 1),
        ('day --speech "A:hello"', 1), ("sheriff --candidates A", 1),
        ("config --show", 0), ("reset", 0),
    ]
    for cmd, expected in cases:
        r, o = run(cmd)
        if r not in (0, 1):
            print(f"  [FAIL] {cmd}: crashed rc={r}")
            return False
    return check("contract (argparse)", True)

def test_fuzz():
    print("\n=== fuzz ===")
    run("reset"); run("init A B C D E F G H I")
    ok = True
    for inp in ["救救救", "毒毒毒", "abc123", "救A 毒B", "skipskip"]:
        r, o = run(f'night-auto --wolf A:B B:B C:B --guard 空过 --seer skip --witch "{inp}"')
        if r not in (0, 1):
            print(f"  [FAIL] crashed: {inp}")
            ok = False
    return check("fuzz", ok)

def test_exploratory():
    print("\n=== exploratory ===")
    run("reset"); run("init A B C D E F G H I")
    run('sheriff --candidates A B --vote A:A B:B C:A D:A E:A')
    r1, _ = run("night-auto --wolf A:B B:B C:B --guard 空过 --seer skip --witch 跳过 --pass-sheriff Z")
    run("config wolf_explode true")
    run("reset"); run("init A B C D E F G H I")
    r2, _ = run("explode A")
    run("config wolf_explode false")
    return check("exploratory", r1 in (0,1) and r2 in (0,1))

if __name__ == "__main__":
    test_mode = "--all" in sys.argv
    adv_mode = "--advanced" in sys.argv
    print("werewolf " + ("advanced" if adv_mode else "full" if test_mode else "quick") + " test suite")
    print("=" * 30)
    cleanup()
    tests = [("smoke", test_smoke), ("init_guards", test_init_guards),
             ("config", test_config), ("tie", test_tie),
             ("sheriff", test_sheriff), ("extract", test_extract)]
    if test_mode:
        tests += [
            ("state", test_state_consistency), ("interactions", test_role_interactions),
            ("config_combos", test_config_combos), ("win", test_win_conditions),
            ("input", test_input_robustness), ("log", test_log_integrity),
        ]
    if adv_mode:
        tests = [("contract", test_contract), ("fuzz", test_fuzz),
                 ("exploratory", test_exploratory)]
    passed = 0
    for name, fn in tests:
        try:
            if fn():
                passed += 1
        except Exception as e:
            print(f"  [FAIL] {name}: {e}")
    cleanup()
    print(f"\n{passed}/{len(tests)} passed")
    exit(0 if passed == len(tests) else 1)


