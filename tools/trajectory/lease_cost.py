"""Reports what the workspace write lease cost, from session wire logs.

The lease is why two sessions cannot write one project at the same time. This
reads the accounts the kernel closes at each release and says how often that
serialisation was felt and how much of each hold was spent writing nothing.

Standard library only.
"""

import argparse
import json
import os
import sys

FRAME_KIND = "workspace_lease"
LOG_SUFFIX = ".wire.jsonl"
META_SUFFIX = ".wire.meta.json"


def wire_logs(root):
    """Every session wire log under root, or root itself when it is one."""
    if os.path.isfile(root):
        return [root] if root.endswith(LOG_SUFFIX) else []
    found = []
    for base, _dirs, files in os.walk(root):
        found.extend(os.path.join(base, f) for f in files if f.endswith(LOG_SUFFIX))
    return sorted(found)


def truncated(log_path):
    """Whether the cap refused a frame in this log.

    A refused frame leaves the log a valid prefix, and a lease account is
    written when a turn ends — so the sessions most likely to lose one are the
    long, noisy sessions, which are also the ones whose numbers matter most.
    An unreadable witness counts as truncated: it may not be reported as intact.
    """
    meta = log_path[: -len(LOG_SUFFIX)] + META_SUFFIX
    try:
        with open(meta, encoding="utf-8") as fh:
            return bool(json.load(fh).get("truncated"))
    except FileNotFoundError:
        return False
    except (OSError, ValueError):
        return True


def holds(log_path):
    """The lease accounts in one log. A line that does not parse is skipped:
    a wire log is append-only and its last line may be a partial write."""
    out = []
    try:
        with open(log_path, encoding="utf-8") as fh:
            for line in fh:
                line = line.strip()
                if not line:
                    continue
                try:
                    frame = json.loads(line)
                except ValueError:
                    continue
                if frame.get("kind") != FRAME_KIND:
                    continue
                account = frame.get("workspaceLease")
                if isinstance(account, dict):
                    out.append(account)
    except OSError:
        return []
    return out


def percentile(values, share):
    """Nearest-rank. An empty population has no percentile, and saying 0 for
    one is how an absent measurement reads as a measured zero."""
    if not values:
        return None
    ordered = sorted(values)
    rank = max(1, min(len(ordered), round(share * len(ordered) + 0.5)))
    return ordered[rank - 1]


def summarise(root):
    logs = wire_logs(root)
    cut = [p for p in logs if truncated(p)]
    accounts = []
    with_holds = 0
    for path in logs:
        found = holds(path)
        if found:
            with_holds += 1
        accounts.extend(found)

    contended = [a for a in accounts if a.get("contended", 0) > 0]
    waited = [a.get("waitedMs", 0) for a in contended]
    held = [a.get("heldMs", 0) for a in accounts]
    # The share of a hold spent holding and not writing. Only holds with a
    # length have one; a zero-length hold is a ratio nobody can take.
    idle_share = [a.get("idleMs", 0) / a["heldMs"] for a in accounts if a.get("heldMs", 0) > 0]

    return {
        "logs": len(logs),
        "logs_with_holds": with_holds,
        "logs_truncated": len(cut),
        "holds": len(accounts),
        "contended_holds": len(contended),
        "reported_holds": len([a for a in accounts if a.get("reported", 0) > 0]),
        "waited_ms_p50": percentile(waited, 0.50),
        "waited_ms_p90": percentile(waited, 0.90),
        "waited_ms_max": max(waited) if waited else None,
        "held_ms_p50": percentile(held, 0.50),
        "held_ms_p90": percentile(held, 0.90),
        "idle_share_p50": percentile(idle_share, 0.50),
        "idle_share_p90": percentile(idle_share, 0.90),
    }


def render(s):
    def num(v, unit=""):
        return "—" if v is None else f"{v}{unit}"

    def pct(v):
        return "—" if v is None else f"{v * 100:.0f}%"

    contended = s["contended_holds"]
    share = f"{contended / s['holds'] * 100:.1f}%" if s["holds"] else "—"
    lines = [
        "What this counts: one account per release of the workspace write lease.",
        "  anchor      waited, from the first failed acquisition to the last;",
        "              held, from acquisition to release; idle, from the last",
        "              call that asked for it to release.",
        "  population  sessions that wrote. A read-only turn never takes the",
        "              lease and is not in the denominator.",
        "  excludes    re-entrant asks inside one session, and worktrees, which",
        "              are separate writer domains.",
        "",
        f"  wire logs                 {s['logs']} ({s['logs_with_holds']} with a hold)",
        f"  incomplete logs           {s['logs_truncated']}",
        f"  holds                     {s['holds']}",
        f"  contended                 {contended}  ({share})",
        f"  of those, felt past 1s    {s['reported_holds']}",
        "",
        f"  waited   p50 {num(s['waited_ms_p50'], 'ms')}   p90 {num(s['waited_ms_p90'], 'ms')}   max {num(s['waited_ms_max'], 'ms')}",
        f"  held     p50 {num(s['held_ms_p50'], 'ms')}   p90 {num(s['held_ms_p90'], 'ms')}",
        f"  idle/held p50 {pct(s['idle_share_p50'])}   p90 {pct(s['idle_share_p90'])}",
    ]
    if s["logs_truncated"]:
        lines += [
            "",
            f"  {s['logs_truncated']} log(s) lost frames to the size cap. A lease account is",
            "  written when a turn ends, so what is missing is biased toward the",
            "  long sessions — read every number above as a floor.",
        ]
    if not s["holds"]:
        lines += ["", "  No hold was recorded. Nothing above is a measurement of anything."]
    return "\n".join(lines)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("root", help="a session directory, or one .wire.jsonl")
    parser.add_argument("--json", action="store_true", help="emit the summary as JSON")
    args = parser.parse_args(argv)

    if not os.path.exists(args.root):
        print(f"no such path: {args.root}", file=sys.stderr)
        return 2
    summary = summarise(args.root)
    print(json.dumps(summary, indent=2, sort_keys=True) if args.json else render(summary))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
