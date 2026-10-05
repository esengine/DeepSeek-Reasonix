"""Delete generated tasks the corpus gates rejected.

Reads `go test -json` on stdin. A task survives only if no gate failed on it —
a rejected bundle is removed rather than patched, so the baseline never learns
to tolerate a grader that measures nothing.
"""
import json
import os
import shutil
import sys

TASKS = os.path.join(os.path.dirname(os.path.abspath(__file__)), "tasks")

failed, passed, why = set(), set(), {}
for line in sys.stdin:
    try:
        ev = json.loads(line)
    except ValueError:
        continue
    name = ev.get("Test") or ""
    parts = name.split("/")
    # Only the train subtests name a generated task: Test/<corpus>/<id>.
    if len(parts) != 3 or parts[1] != "train":
        continue
    task = parts[2]
    if ev.get("Action") == "fail":
        failed.add(task)
    elif ev.get("Action") == "pass":
        passed.add(task)
    elif ev.get("Action") == "output":
        # Keep the assertion message, not the "--- FAIL:" banner around it.
        text = ev.get("Output", "").strip()
        if text.startswith(("---", "===", "ok ", "FAIL")) or ".go:" not in text:
            continue
        if task not in why:
            why[task] = text.split(": ", 1)[-1][:110]

for task in sorted(failed):
    path = os.path.join(TASKS, task)
    shutil.rmtree(path, ignore_errors=True)
    print("rejected %-24s %s" % (task, why.get(task, "gate failed")))

kept = sorted(passed - failed)
print("\nkept %d, rejected %d" % (len(kept), len(failed)))
for task in kept:
    print("  " + task)
sys.exit(0)
