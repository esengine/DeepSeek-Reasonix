#!/usr/bin/env python3
"""Select the published SWE-bench Verified subset.

Deterministic by construction: no random seed, no hand-picking. Re-running this
against the same dataset revision reproduces subset.json byte for byte, so the
sample cannot be quietly tuned after seeing results.

Rules:
  1. Repos get slots in proportion to their share of the full 500, largest
     remainder first, so the sample keeps the benchmark's real composition
     (django is ~46% of SWE-bench Verified and stays ~46% here).
  2. Within a repo, instances are ordered by (difficulty, instance_id) and
     picked at evenly spaced indices, which spreads the sample across the
     repo's own difficulty mix instead of clustering on easy ones.

Nothing is excluded. psf/requests instances exercise a test suite that makes
live network calls and can exhaust the grader timeout; such a run is reported
as eval_timeout rather than dropped, because silently removing the instances a
harness handles badly is how a benchmark stops meaning anything.
"""

import json
import sys
from datasets import load_dataset

TOTAL = 50
DIFFICULTY_ORDER = {"<15 min fix": 0, "15 min - 1 hour": 1, "1-4 hours": 2, ">4 hours": 3}


def allocate(counts, total):
    """Largest-remainder apportionment of `total` slots across repo counts."""
    population = sum(counts.values())
    exact = {repo: n * total / population for repo, n in counts.items()}
    floors = {repo: int(v) for repo, v in exact.items()}
    remaining = total - sum(floors.values())
    order = sorted(exact, key=lambda r: (-(exact[r] - floors[r]), r))
    for repo in order[:remaining]:
        floors[repo] += 1
    return floors


def evenly_spaced(items, k):
    if k <= 0:
        return []
    if k >= len(items):
        return items
    return [items[(2 * i + 1) * len(items) // (2 * k)] for i in range(k)]


def main():
    ds = load_dataset("princeton-nlp/SWE-bench_Verified", split="test")
    by_repo = {}
    for row in ds:
        by_repo.setdefault(row["repo"], []).append(row)

    slots = allocate({repo: len(rows) for repo, rows in by_repo.items()}, TOTAL)

    selected = []
    for repo in sorted(by_repo):
        rows = sorted(
            by_repo[repo],
            key=lambda r: (DIFFICULTY_ORDER.get(r["difficulty"], 9), r["instance_id"]),
        )
        selected.extend(evenly_spaced(rows, slots[repo]))

    selected.sort(key=lambda r: r["instance_id"])
    out = [
        {
            "instance_id": r["instance_id"],
            "repo": r["repo"],
            "base_commit": r["base_commit"],
            "problem_statement": r["problem_statement"],
            "difficulty": r["difficulty"],
        }
        for r in selected
    ]
    with open("benchmarks/swebench/subset.json", "w", encoding="utf-8") as f:
        json.dump(out, f, indent=2, ensure_ascii=False)
        f.write("\n")

    print(f"selected {len(out)} of {len(ds)}", file=sys.stderr)
    for repo in sorted(by_repo):
        share = 100 * len(by_repo[repo]) / len(ds)
        print(f"  {slots[repo]:2d}  {repo:32s} (full set {share:4.1f}%)", file=sys.stderr)


if __name__ == "__main__":
    main()
