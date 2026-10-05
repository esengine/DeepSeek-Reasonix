import importlib, pathlib, sys
sys.path.insert(0, ".")
fail = []

from svc.query.filters import build_filter
if build_filter("x=1", ["a=2", "b=3"], "any") != "x=1 AND (a=2 OR b=3)":
    fail.append("query: OR clauses are not parenthesised")
if build_filter("x=1", ["a=2", "b=3"], "all") != "x=1 AND (a=2 AND b=3)":
    fail.append("query: AND clauses are not parenthesised")
if build_filter("x=1", ["a=2"], "any") != "x=1 AND a=2":
    fail.append("query: a single clause must not be parenthesised")

from svc.format.human import humanise
for n, want in ((1250, "1.3k"), (1234, "1.2k"), (3450000, "3.5M"), (999, "999")):
    if humanise(n) != want:
        fail.append(f"format: humanise({n}) = {humanise(n)!r}, want {want!r}")

expected = []
for pkg in ("query", "format"):
    for p in sorted(pathlib.Path("svc", pkg).glob("*.py")):
        if p.stem == "__init__":
            continue
        m = importlib.import_module(f"svc.{pkg}.{p.stem}")
        for name in sorted(dir(m)):
            fn = getattr(m, name)
            if callable(fn) and not name.startswith("_") and getattr(fn, "__module__", "") == m.__name__:
                expected.append(f"- {pkg}.{name}")
expected = sorted(set(expected))
got = sorted(l.strip() for l in pathlib.Path("docs/api.md").read_text().splitlines() if l.startswith("- "))
if got != expected:
    missing = [e for e in expected if e not in got]
    extra = [g for g in got if g not in expected]
    fail.append(f"docs: manifest is stale (missing {len(missing)}, extra {len(extra)})")

if fail:
    print("FAIL:"); [print(" ", f) for f in fail]; sys.exit(1)
print("OK")
