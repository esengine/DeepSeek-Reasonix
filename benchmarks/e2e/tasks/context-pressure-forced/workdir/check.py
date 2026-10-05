import importlib, pathlib, sys
sys.path[:0] = ["common", "."]
bad = []
for pkg in ("alpha", "beta", "gamma"):
    for p in sorted(pathlib.Path("pkgs", pkg).glob("*.py")):
        if p.stem == "__init__":
            continue
        src = p.read_text()
        if "fmt_amount(" not in src:
            continue
        m = importlib.import_module(f"pkgs.{pkg}.{p.stem}")
        for name in dir(m):
            fn = getattr(m, name)
            # Only functions this module defines: fmt_amount is imported into
            # the namespace and is not one of the call sites under test.
            if not callable(fn) or name.startswith("_") or getattr(fn, "__module__", "") != m.__name__:
                continue
            try:
                got = fn([{"cents": 1234}])
            except TypeError as exc:
                bad.append(f"{pkg}/{p.stem}.{name}: {exc}")
                continue
            if got and got[0] not in ("1234", "12.34 USD"):
                bad.append(f"{pkg}/{p.stem}.{name}: {got[0]}")
if bad:
    print("FAIL:"); [print(" ", b) for b in bad[:10]]; sys.exit(1)
print("OK")
