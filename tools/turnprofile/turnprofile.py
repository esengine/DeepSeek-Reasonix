#!/usr/bin/env python3
"""Splits each turn of a `--trajectory` JSONL into exclusive time buckets."""

import argparse
import json
import statistics
import sys

SCHEMA_VERSION = 1

# Highest priority first: a millisecond covered by several intervals goes to
# the earliest bucket here, so the buckets partition the turn.
PRIORITY = ["user_wait", "compaction", "tool", "model_discarded", "model_ttft", "model_gen"]
HOST = ["host_setup", "host_between_rounds", "host_tail"]
BUCKETS = PRIORITY + HOST


def load(path):
    records = []
    with open(path, encoding="utf-8") as fh:
        for line in fh:
            line = line.strip()
            if line:
                records.append(json.loads(line))
    return records


def events(records):
    for r in records:
        e = r.get("event")
        if e:
            yield r["ts"], r.get("end_ts") or r["ts"], e
        elif r.get("turn_completion"):
            yield r["ts"], r["ts"], {"kind": "turn_done"}


def split_turns(records):
    """Yields (start_ts, end_ts, censored, events) per turn."""
    turns, cur = [], None
    for ts, end, e in events(records):
        kind = e.get("kind")
        if kind == "turn_started":
            if cur is not None:
                turns.append((cur[0], cur[2], True, cur[1]))
            cur = [ts, [], ts]
        if cur is None:
            continue
        cur[1].append((ts, end, e))
        cur[2] = max(cur[2], end)
        if kind == "turn_done":
            turns.append((cur[0], end, False, cur[1]))
            cur = None
    if cur is not None:
        turns.append((cur[0], cur[2], True, cur[1]))
    return turns


def intervals(turn_events, t_end):
    """Returns ({bucket: [(a, b)]}, rounds, tools, approvals)."""
    out = {b: [] for b in PRIORITY}
    rounds, tools = [], []
    begins, comp_open, waits = {}, None, []
    started_by_id = {}
    for ts, end, e in turn_events:
        kind = e.get("kind")
        if kind == "stream_attempt":
            sa = e.get("streamAttempt") or {}
            act, sid = sa.get("action"), sa.get("id")
            if act == "begin":
                begins[sid] = ts
            elif act in ("commit", "discard") and sid in begins:
                a, b = begins.pop(sid), ts
                if act == "discard":
                    out["model_discarded"].append((a, b))
                else:
                    ft = sa.get("firstTokenMs") or 0
                    mid = min(b, a + ft) if ft else a
                    if ft:
                        out["model_ttft"].append((a, mid))
                    out["model_gen"].append((mid, b))
                    rounds.append({"start": a, "end": b, "ttft_ms": ft or None})
        elif kind == "tool_result":
            t = e.get("tool") or {}
            s, f = t.get("startedAt"), t.get("endedAt")
            if s and f and f >= s:
                out["tool"].append((s, f))
                started_by_id[t.get("id")] = s
                tools.append({"name": t.get("resolvedName") or t.get("name"), "ms": f - s, "start": s, "end": f})
        elif kind == "compaction_started":
            comp_open = ts
        elif kind == "compaction_done" and comp_open is not None:
            out["compaction"].append((comp_open, ts))
            comp_open = None
        elif kind in ("approval_request", "ask_request"):
            waits.append((ts, (e.get("approval") or e.get("ask") or {}).get("id"), (e.get("approval") or {}).get("toolId")))
    for sid, a in begins.items():
        out["model_discarded"].append((a, t_end))
    if comp_open is not None:
        out["compaction"].append((comp_open, t_end))
    for ts, _, tool_id in waits:
        nxt = started_by_id.get(tool_id)
        out["user_wait"].append((ts, nxt if nxt and nxt >= ts else t_end))
    return out, rounds, tools


def partition(t0, t1, ivs, rounds):
    """Assigns every millisecond of [t0, t1] to exactly one bucket."""
    cuts = {t0, t1}
    for lst in ivs.values():
        for a, b in lst:
            cuts.update(x for x in (a, b) if t0 <= x <= t1)
    first = min((r["start"] for r in rounds), default=t1)
    last = max((r["end"] for r in rounds), default=t0)
    for ts_ in ivs["tool"]:
        last = max(last, ts_[1]) if ts_[1] <= t1 else last
    cuts = sorted(cuts)
    totals = {b: 0 for b in BUCKETS}
    for a, b in zip(cuts, cuts[1:]):
        mid = (a + b) / 2
        for name in PRIORITY:
            if any(x <= mid < y for x, y in ivs[name]):
                totals[name] += b - a
                break
        else:
            if mid < first:
                totals["host_setup"] += b - a
            elif mid >= last:
                totals["host_tail"] += b - a
            else:
                totals["host_between_rounds"] += b - a
    return totals


def profile(records):
    turns = []
    for t0, t1, censored, evs in split_turns(records):
        ivs, rounds, tools = intervals(evs, t1)
        totals = partition(t0, t1, ivs, rounds)
        busy = sum(x["ms"] for x in tools)
        turns.append({
            "wall_ms": t1 - t0,
            "censored": censored,
            "buckets_ms": totals,
            "rounds": len(rounds),
            "tools": len(tools),
            "tool_busy_ms": busy,
            "ttft_ms": [r["ttft_ms"] for r in rounds if r["ttft_ms"]],
            "tool_calls": tools,
        })
    return turns


def pct(vals, p):
    if not vals:
        return None
    vals = sorted(vals)
    return vals[min(len(vals) - 1, int(round(p * (len(vals) - 1))))]


def aggregate(turns):
    done = [t for t in turns if not t["censored"]]
    wall = sum(t["wall_ms"] for t in done)
    sums = {b: sum(t["buckets_ms"][b] for t in done) for b in BUCKETS}
    by_tool = {}
    for t in done:
        for c in t["tool_calls"]:
            by_tool.setdefault(c["name"], []).append(c["ms"])
    ttft = [x for t in done for x in t["ttft_ms"]]
    return {
        "schema_version": SCHEMA_VERSION,
        "turns": len(done),
        "censored_turns": len(turns) - len(done),
        "wall_ms": wall,
        "buckets_ms": sums,
        "ttft_ms": {"n": len(ttft), "p50": pct(ttft, .5), "p90": pct(ttft, .9), "max": max(ttft, default=None)},
        "tools": {
            n: {"n": len(v), "total_ms": sum(v), "p50": pct(v, .5), "p90": pct(v, .9), "max": max(v)}
            for n, v in sorted(by_tool.items(), key=lambda kv: -sum(kv[1]))
        },
        "tool_busy_ms": sum(t["tool_busy_ms"] for t in done),
    }


def render(agg):
    w = agg["wall_ms"] or 1
    lines = [f"turns={agg['turns']} (censored, excluded: {agg['censored_turns']})  wall={agg['wall_ms'] / 1000:.1f}s"]
    for b in BUCKETS:
        ms = agg["buckets_ms"][b]
        lines.append(f"  {b:<20}{ms / 1000:>9.1f}s {100 * ms / w:>5.1f}%")
    f = agg["ttft_ms"]
    if f["n"]:
        lines.append(f"first token: n={f['n']} p50={f['p50']}ms p90={f['p90']}ms max={f['max']}ms")
    lines.append(f"tool wall (union)={agg['buckets_ms']['tool'] / 1000:.1f}s  tool busy (sum)={agg['tool_busy_ms'] / 1000:.1f}s")
    for n, s in list(agg["tools"].items())[:10]:
        lines.append(f"  {n:<24}n={s['n']:<4}total={s['total_ms'] / 1000:>7.1f}s p50={s['p50']}ms p90={s['p90']}ms max={s['max']}ms")
    return "\n".join(lines)


def main(argv=None):
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("files", nargs="+")
    ap.add_argument("--json", action="store_true")
    ap.add_argument("--per-turn", action="store_true")
    args = ap.parse_args(argv)
    turns = []
    for f in args.files:
        turns.extend(profile(load(f)))
    agg = aggregate(turns)
    if args.json:
        json.dump({"aggregate": agg, "turns": turns if args.per_turn else None}, sys.stdout, ensure_ascii=False, indent=1)
        print()
        return 0
    print(render(agg))
    if args.per_turn:
        for i, t in enumerate(turns, 1):
            top = max(BUCKETS, key=lambda b: t["buckets_ms"][b])
            print(f"turn {i}: wall={t['wall_ms'] / 1000:.1f}s rounds={t['rounds']} tools={t['tools']} top={top} censored={t['censored']}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
