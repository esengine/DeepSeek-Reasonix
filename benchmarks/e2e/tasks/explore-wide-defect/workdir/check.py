import importlib, sys, pathlib
sys.path.insert(0, "pkg")
bad = []
for p in sorted(pathlib.Path("pkg").glob("*.py")):
    m = importlib.import_module(p.stem)
    if m.normalise("  ready:  ") != "ready":
        bad.append(p.stem)
if bad:
    print("FAIL:", ", ".join(bad)); sys.exit(1)
print("OK")
