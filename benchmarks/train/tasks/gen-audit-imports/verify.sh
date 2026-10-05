#!/usr/bin/env bash
# Grader for the "module import report" task.
# Runs from a directory holding the project contents at the top level.
set -euo pipefail

cd "$(dirname "$0")"

python3 - <<'PY'
import ast
import hashlib
import pathlib
import sys

root = pathlib.Path(".")

EXPECTED = [
    "app/config -> os",
    "app/main -> app.config",
    "app/main -> app.models",
    "app/main -> app.utils",
    "app/models -> dataclasses.dataclass",
    "app/utils -> app.models.Item",
    "app/utils -> json",
    "app/utils -> zoneinfo",
    "scripts/standalone -> app.main.run",
    "scripts/standalone -> sys",
]

# sha256 of the seed files; any modification to the project source fails.
EXPECTED_HASHES = {
    "app/__init__.py": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
    "app/config.py": "7d5203d285bcc073547f9cb9648ef59b8269ed5b40f6c081dafb99fc6dc3cdde",
    "app/main.py": "7a795061ccac9e67961dee830344473fcc1ce3844a6655956c8443f3ea81007b",
    "app/models.py": "8d4198656a4e40194a278c3aeb7dcde3f8685d860821c9397a9171ef654286c8",
    "app/utils.py": "c54cfb01d46373e7460918b3a04d764227f6d15886dd1252977afcaad21643ea",
    "scripts/standalone.py": "f2889249515f36757ba3bb8840b9b16329ccececb49fecaefc369875ea1e29c3",
}


def imports_in(path):
    tree = ast.parse(path.read_text())
    found = set()
    for node in ast.walk(tree):
        if isinstance(node, ast.Import):
            for alias in node.names:
                found.add(alias.name)
        elif isinstance(node, ast.ImportFrom) and node.level == 0:
            for alias in node.names:
                found.add(f"{node.module}.{alias.name}")
    return found


failures = []

report_path = root / "import_report.txt"
if not report_path.exists():
    print("VERIFY FAILED: import_report.txt was not created at the project root")
    sys.exit(1)

lines = [ln.strip() for ln in report_path.read_text().splitlines() if ln.strip()]

if lines != sorted(EXPECTED):
    failures.append("import_report.txt content is wrong (wrong lines, wrong order, or duplicates)")
    report_set = set(lines)
    print("  missing:", sorted(set(EXPECTED) - report_set))
    print("  extra:  ", sorted(report_set - set(EXPECTED)))

for path, expected_hash in EXPECTED_HASHES.items():
    p = root / path
    if not p.exists():
        failures.append(f"{path} is missing (a source file was deleted)")
        continue
    if hashlib.sha256(p.read_bytes()).hexdigest() != expected_hash:
        failures.append(f"{path} was modified; the task forbids changing source files")

scan = {}
for p in sorted(root.rglob("*.py")):
    rel = p.relative_to(root).as_posix()
    if rel.endswith(".py"):
        rel = rel[:-3]
    scan.setdefault(rel, set()).update(imports_in(p))

reported = {}
for ln in lines:
    importer, sep, imported = ln.partition(" -> ")
    if not sep:
        failures.append(f"malformed report line: {ln!r}")
        continue
    reported.setdefault(importer, set()).add(imported)

for importer in sorted(set(scan) | set(reported)):
    if reported.get(importer, set()) != scan.get(importer, set()):
        failures.append(f"report for {importer!r} does not match the imports actually present in the tree")

if failures:
    print("VERIFY FAILED:")
    for f in failures:
        print(" -", f)
    sys.exit(1)

print("VERIFY PASSED: import_report.txt is correct and no source file was changed")
PY
