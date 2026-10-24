import argparse
import json
import math
from pathlib import Path, PureWindowsPath
import statistics


def merged(spans):
    result = []
    for start, end in sorted(spans):
        if start < 0 or end < start:
            raise ValueError("invalid monotonic interval")
        if result and start <= result[-1][1]:
            result[-1] = (result[-1][0], max(end, result[-1][1]))
        else:
            result.append((start, end))
    return result


def duration(spans):
    return sum(end - start for start, end in merged(spans))


def overlap(a, b):
    return sum(max(0, min(a1, b1) - max(a0, b0)) for a0, a1 in merged(a) for b0, b1 in merged(b))


def percentile(values):
    return {"median_ms": statistics.median(values) / 1e6,
            "p95_ms": sorted(values)[math.ceil(0.95 * len(values)) - 1] / 1e6}


def worker(row, platform, recorded):
    inference = [(p["start_ns"], p["end_ns"]) for p in row["phases"] if p["phase"] == "provider_wait"]
    waits = [(p["start_ns"], p["end_ns"]) for p in row["phases"] if p["phase"] == "resource_wait"]
    tools = [(p["start_ns"], p["end_ns"]) for p in row["phases"] if p["phase"] == "tool_dispatch_to_result"]
    common = {"inference_ns": duration(inference), "tool_dispatch_ns": duration(tools)}
    if row["effective_policy"] == "shared":
        common["notified_resource_wait_ns"] = duration(waits)
    if not recorded:
        return common
    lower, upper, registry, preparation = [], [], [], []
    path_type = PureWindowsPath if platform.startswith("windows/") else Path
    state = path_type(row["coordination_dir"])
    for lock in row.get("locks", []):
        if not 0 <= lock["attempt_ns"] <= lock["acquired_ns"] <= lock["releasing_ns"] <= lock["released_ns"] or lock["released_ns"] == 0:
            raise ValueError("unclosed or invalid OS lock")
        path = path_type(lock["path"])
        if not path.is_relative_to(state):
            continue
        relative = path.relative_to(state)
        span = (lock["acquired_ns"], lock["releasing_ns"])
        if relative.name == "registry.lock" or relative.name.endswith(".guard"):
            registry.append(span)
        elif relative.name.endswith(".preparing"):
            preparation.append(span)
        elif not lock["shared"] and (".claims/" in relative.as_posix() or (len(relative.parts) == 1 and relative.name.endswith(".lock"))):
            lower.append(span)
            upper.append((lock["attempt_ns"], lock["released_ns"]))
    if recorded and not lower:
        raise ValueError("recorded worker has no recognized resource holds")
    return {**common,
            "resource_hold_lower_ns": duration(lower), "resource_hold_upper_ns": duration(upper),
            "registry_ns": duration(registry), "preparation_ns": duration(preparation),
            "resource_inference_overlap_ns": overlap(upper, inference)}


def summarize(report):
    if not report.get("completed", True):
        raise ValueError("incomplete qualification: " + report.get("failure", "still running"))
    output = {"summary_schema": 2, "platform": report["platform"], "load": report["load"], "definition": report["definition"],
              "wait_scope": "Notified transaction waits only. Legacy waiting is unavailable here; unnotified waiting remains included in tool dispatch to result.", "results": {}}
    for key, rounds in report["results"].items():
        samples = []
        for sample in rounds:
            if len(sample["workers"]) != report["load"]["workers"] or len({w["id"] for w in sample["workers"]}) != report["load"]["workers"]:
                raise ValueError("missing or duplicate real workers")
            row = {"wall_ns": sample["wall_ns"], "conflicts": 0, "reschedules": 0}
            for w in sample["workers"]:
                if w.get("effective_policy") != key.split("/")[-1]:
                    raise ValueError("worker did not prove its assembled runtime policy")
                for name, value in worker(w, report["platform"], report["load"]["record_locks"]).items():
                    row[name] = row.get(name, 0) + value
                row["conflicts"] += w["conflicts"]
                row["reschedules"] += w["reschedules"]
            if key.endswith("/shared") and row.get("resource_inference_overlap_ns", 0):
                raise ValueError("shared product resource lock overlaps provider inference")
            samples.append(row)
        result = {name: percentile([s[name] for s in samples]) for name in samples[0] if name.endswith("_ns")}
        result.update(trials=len(samples), conflicts=sum(s["conflicts"] for s in samples), reschedules=sum(s["reschedules"] for s in samples))
        output["results"][key] = result
    return output


def self_test():
    try:
        summarize({"completed": False, "failure": "worker preparation failed"})
    except ValueError:
        pass
    else:
        raise AssertionError("incomplete qualification reported as complete")
    assert duration([(1, 5), (3, 8), (10, 12)]) == 9
    assert overlap([(1, 5), (3, 8)], [(0, 2), (4, 6)]) == 3
    assert overlap([(1, 2)], [(2, 3)]) == 0
    try:
        duration([(2, 1)])
    except ValueError:
        pass
    else:
        raise AssertionError("invalid spans accepted")
    row = {"id": 0, "effective_policy": "shared", "conflicts": 0, "reschedules": 0,
           "phases": [], "coordination_dir": "/coord", "locks": []}
    assert "resource_hold_lower_ns" not in worker(row, "linux/arm64", False)
    legacy = {**row, "effective_policy": "legacy"}
    assert "notified_resource_wait_ns" not in worker(legacy, "linux/arm64", False)
    assert worker(row, "linux/arm64", False)["notified_resource_wait_ns"] == 0
    try:
        worker(row, "linux/arm64", True)
    except ValueError:
        pass
    else:
        raise AssertionError("absent OS recording treated as measured zero")
    row["phases"] = [{"phase": "provider_wait", "start_ns": 10, "end_ns": 20}]
    row["locks"] = [{"path": "/coord/workspace.lock", "shared": False,
                     "attempt_ns": 5, "acquired_ns": 8, "releasing_ns": 12, "released_ns": 15}]
    report = {"platform": "linux/arm64", "definition": "self-test", "load": {"workers": 1, "record_locks": True},
              "results": {"named/shared": [{"wall_ns": 30, "workers": [row]}]}}
    try:
        summarize(report)
    except ValueError:
        pass
    else:
        raise AssertionError("shared resource hold overlapping inference accepted")


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("report", type=Path, nargs="?")
    parser.add_argument("--output", type=Path)
    parser.add_argument("--self-test", action="store_true")
    args = parser.parse_args()
    if args.self_test:
        self_test()
    if args.report:
        body = json.dumps(summarize(json.loads(args.report.read_text())), indent=2) + "\n"
        if args.output:
            args.output.write_text(body)
        else:
            print(body, end="")
