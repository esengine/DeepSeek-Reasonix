#!/usr/bin/env python3
"""Compare blocked Acquire scans with only the coordinator caller replaced.

Run from the repository root. This compiles disposable Go overlays; it does not
rewrite product files. Metrics describe same-process waiters, not a product run.
"""

import argparse
import collections
import itertools
import json
import math
import os
from pathlib import Path
import platform
import random
import resource
import statistics
import subprocess
import tempfile
import time


def run(args):
    root = Path.cwd()
    out = Path(args.out).resolve()
    out.mkdir(parents=True, exist_ok=False)
    owner = root / "internal/state/mutation"
    fixture = root / "scripts/testdata/mutation-wait-probe.go.txt"
    baseline = subprocess.check_output(
        ["git", "show", f"{args.baseline}:internal/state/mutation/coordinator.go"],
        text=True,
    )
    registry = (owner / "registry.go").read_text()
    anchors = {
        "func (c *Coordinator) records() ([]record, error) {":
            "func (c *Coordinator) records() ([]record, error) {\n\tqueueDiagnosticScans.Add(1)",
        "\t\tdata, err := os.ReadFile(c.recordPath(id))":
            "\t\tqueueDiagnosticReads.Add(1)\n\t\tdata, err := os.ReadFile(c.recordPath(id))",
    }
    for old, new in anchors.items():
        if registry.count(old) != 1:
            raise RuntimeError("Registry changed; review instrumentation anchors")
        registry = registry.replace(old, new)
    rows = []
    load_start = os.getloadavg()
    with tempfile.TemporaryDirectory(prefix="mutation-wait-") as temp:
        temp = Path(temp)
        (temp / "before.go").write_text(baseline)
        (temp / "registry.go").write_text(registry)
        for variant in ("before", "after"):
            replace = {
                str(owner / "queue_cost_probe_test.go"): str(fixture),
                str(owner / "registry.go"): str(temp / "registry.go"),
            }
            if variant == "before":
                replace[str(owner / "coordinator.go")] = str(temp / "before.go")
            overlay = temp / f"{variant}.json"
            overlay.write_text(json.dumps({"Replace": replace}))
            subprocess.run(
                ["go", "test", "-c", "-overlay", str(overlay), "-o",
                 str(temp / variant), "./internal/state/mutation"], check=True,
            )
        cases = list(itertools.product(
            ("before", "after"), (1, 16, 64), (0, 1, 8, 32), range(args.samples)
        ))
        random.Random(14025).shuffle(cases)
        for variant, holders, waiters, trial in cases:
            env = dict(os.environ, RX_QUEUE_HOLDERS=str(holders), RX_QUEUE_WAITERS=str(waiters))
            before = resource.getrusage(resource.RUSAGE_CHILDREN)
            started = time.monotonic()
            result = subprocess.run(
                [str(temp / variant), "-test.run=^TestQueueCostDiagnostic$",
                 "-test.v", "-test.timeout=45s"], env=env, capture_output=True, text=True,
            )
            elapsed = time.monotonic() - started
            after = resource.getrusage(resource.RUSAGE_CHILDREN)
            log = f"{variant}-{holders}-{waiters}-{trial}.log"
            (out / log).write_text(result.stdout + result.stderr)
            row = {"variant": variant, "holders": holders, "waiters": waiters,
                   "trial": trial, "exit_code": result.returncode, "raw_log": log,
                   "elapsed_ms": elapsed * 1000,
                   "cpu_ms": ((after.ru_utime - before.ru_utime)
                              + (after.ru_stime - before.ru_stime)) * 1000}
            for line in result.stdout.splitlines():
                if line.startswith("RX_QUEUE_METRICS "):
                    row.update(json.loads(line.removeprefix("RX_QUEUE_METRICS ")))
            rows.append(row)
            (out / "raw.json").write_text(json.dumps(rows, indent=2) + "\n")
            if result.returncode or "registry_scans" not in row:
                raise RuntimeError(f"Probe failed; inspect {log}")
    groups = collections.defaultdict(list)
    for row in rows:
        groups[row["variant"], row["holders"], row["waiters"]].append(row)
    metrics = ("elapsed_ms", "cpu_ms", "queue_ready_ms", "cancel_p50_ms",
               "cancel_p95_ms", "registry_scans", "record_reads", "actual_observe_ms")
    summary = []
    for (variant, holders, waiters), group in sorted(groups.items()):
        entry = {"variant": variant, "holders": holders, "waiters": waiters,
                 "samples": len(group)}
        for metric in metrics:
            values = sorted(row[metric] for row in group)
            entry[metric] = {"p50": statistics.median(values),
                             "p95": values[math.ceil(len(values) * 0.95) - 1]}
        summary.append(entry)
    report = {"baseline_caller": args.baseline, "platform": platform.platform(),
              "logical_cpus": os.cpu_count(), "load_start": load_start,
              "load_end": os.getloadavg(), "summary": summary,
              "scope": "Same current source except baseline coordinator caller; identical counter overlay. "
                       "300ms observation after waiting notices; CPU/elapsed include setup and cleanup. "
                       "Fresh child per sample; holders/waiters within one process. Seed14025. "
                       "Three samples make nearest-rank P95 the maximum. No idle-host, cross-process "
                       "contention, native Windows, throughput or final-product qualification."}
    (out / "summary.json").write_text(json.dumps(report, indent=2) + "\n")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--baseline", required=True, help="Commit supplying the preceding coordinator caller")
    parser.add_argument("--out", required=True, help="New output directory; never overwritten")
    parser.add_argument("--samples", type=int, default=3)
    options = parser.parse_args()
    if options.samples < 1:
        parser.error("--samples must be positive")
    run(options)
