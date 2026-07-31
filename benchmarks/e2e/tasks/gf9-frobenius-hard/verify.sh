set -e
python3 - <<'PY'
import sys, os
sys.path.insert(0, os.getcwd())
# If gf9.py exposes helper functions, import and re-verify independently.
try:
    import gf9
    add, mul, pow3 = gf9.add, gf9.mul, gf9.pow3
except AttributeError:
    add, mul, pow3 = gf9.gf9_add, gf9.gf9_mul, gf9.gf9_pow3
except ImportError:
    # Fall back to verifying answer.txt only if no importable module.
    got = open("answer.txt").read().strip().split()
    assert len(got) == 2 and got[0] == "81" and got[1] == "81", f"answer.txt: {got}"
    raise SystemExit(0)
els = [(a, b) for a in range(3) for b in range(3)]
# independent exhaustive verification
for x in els:
    for y in els:
        assert add(x, y) == add(y, x)
        assert mul(x, y) == mul(y, x)
        assert pow3(add(x, y)) == add(pow3(x), pow3(y)), f"add fails {x} {y}"
        assert pow3(mul(x, y)) == mul(pow3(x), pow3(y)), f"mul fails {x} {y}"
img = [pow3(x) for x in els]
assert len(set(img)) == 9, "Frobenius not bijective"
# σ(α) = -α : α = (0,1) → (0,2)
alpha = (0, 1)
assert pow3(alpha) == (0, 2), f"sigma(alpha)={pow3(alpha)}"
print("INDEPENDENT VERIFY OK")
PY
echo OK
