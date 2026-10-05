#!/usr/bin/env python3
"""Extract execution-progress metrics from a Reasonix trajectory export.

Total task tokens measure resource consumption; the gaps between host-observed
progress measure liveness. They are not interchangeable, and this tool reports
both without judging either: it answers what happened, never what should stop.

Two sources of progress exist, and which one a sample used is part of the
sample. The semantic source is the todo_progress frame, whose progress
revision advances only when the canonical task list moved. The proxy source
counts successful complete_step calls, which is what an export predating that
frame can offer; it overcounts, because a sign-off landing on an already
complete step advances nothing. Proxy samples belong to incident analysis and
must not be mixed with semantic ones when calibrating a stopping policy.
"""

import argparse
import json
import re
import sys

SCHEMA_VERSION = 1

_USAGE = re.compile(r"hit ([\d.]+)([km]?) · miss ([\d.]+)([km]?) · out ([\d.]+)([km]?)")
_TODO = re.compile(
    r"^todo (\w+) · content (\d+) · plan (\d+) · progress (\d+)$"
)


class TrajectoryError(Exception):
    """The export cannot be read as a trajectory, or reads as a broken one."""


def _tokens(value: str, unit: str) -> float:
    return float(value) * {"k": 1e3, "m": 1e6}.get(unit, 1)


def _rows(path: str):
    with open(path, encoding="utf-8") as fh:
        doc = json.load(fh)
    if not isinstance(doc, dict) or not isinstance(doc.get("rows"), list):
        raise TrajectoryError(f"{path}: not a trajectory export (no rows array)")
    return doc["rows"], float(doc.get("span") or 0)


def _parse_todo(text: str):
    """Return (kind, content, plan, progress) for a todo_progress line.

    A line that starts like one and does not parse is an error, not a miss: the
    renderer changed shape and every later sample would silently read as having
    no semantic progress at all.
    """
    match = _TODO.match(text)
    if match:
        return match.group(1), int(match.group(2)), int(match.group(3)), int(match.group(4))
    if text.startswith("todo "):
        raise TrajectoryError(f"todo_progress line does not parse: {text!r}")
    return None


def extract(path: str):
    rows, span = _rows(path)
    transitions = []
    cumulative = 0.0
    content = plan = progress = 0
    semantic = False

    for row in rows:
        kind = row.get("kind")
        text = row.get("text") or ""
        if kind == "model_round":
            usage = _USAGE.search(text)
            if usage:
                cumulative += (
                    _tokens(usage.group(1), usage.group(2))
                    + _tokens(usage.group(3), usage.group(4))
                    + _tokens(usage.group(5), usage.group(6))
                )
            continue
        parsed = _parse_todo(text) if kind == "outcome_progress" else None
        if parsed:
            semantic = True
            verdict, content, plan, progress = parsed
            transitions.append({
                "at": row.get("at", 0.0), "cumulative_tokens": cumulative,
                "kind": verdict, "content_rev": content, "plan_rev": plan,
                "progress_rev": progress,
            })

    if semantic:
        return _summarise(transitions, cumulative, span, "todo_progress", "semantic")
    return _summarise(_proxy(rows), cumulative, span, "complete_step", "proxy")


def _proxy(rows):
    """Rebuild transitions from the frames an older export does carry."""
    transitions = []
    cumulative = 0.0
    content = progress = 0
    for row in rows:
        if row.get("kind") == "model_round":
            usage = _USAGE.search(row.get("text") or "")
            if usage:
                cumulative += (
                    _tokens(usage.group(1), usage.group(2))
                    + _tokens(usage.group(3), usage.group(4))
                    + _tokens(usage.group(5), usage.group(6))
                )
        elif row.get("kind") == "tool":
            tool = row.get("tool")
            if tool == "complete_step":
                progress += 1
                content += 1
                transitions.append({
                    "at": row.get("at", 0.0), "cumulative_tokens": cumulative,
                    "kind": "advance", "content_rev": content, "plan_rev": 0,
                    "progress_rev": progress,
                })
            elif tool == "todo_write":
                content += 1
    return transitions


def _quantile(values, q):
    if not values:
        return 0.0
    ordered = sorted(values)
    return ordered[min(int(len(ordered) * q), len(ordered) - 1)]


def _summarise(transitions, cumulative, span, source, fidelity):
    advances = [t for t in transitions if t["kind"] in ("advance", "terminal")]
    out = {
        "schema_version": SCHEMA_VERSION,
        "progress_source": source,
        "progress_fidelity": fidelity,
        "total_tokens": cumulative,
        "wall_total_s": span,
        "progress_events": len(advances),
        "content_revisions": transitions[-1]["content_rev"] if transitions else 0,
        "plan_revisions": transitions[-1]["plan_rev"] if transitions else 0,
        "progress_revisions": transitions[-1]["progress_rev"] if transitions else 0,
    }
    if not advances:
        out.update({
            "p50_tokens_between_progress": None, "p90_tokens_between_progress": None,
            "max_tokens_between_progress": None, "max_wall_between_progress_s": None,
            "max_to_p50_ratio": None, "tokens_after_last_progress": cumulative,
            "wall_after_last_progress_s": span, "content_to_progress_ratio": None,
        })
        return out, transitions

    token_gaps, wall_gaps = [], []
    previous_tokens = previous_at = 0.0
    for advance in advances:
        token_gaps.append(advance["cumulative_tokens"] - previous_tokens)
        wall_gaps.append(advance["at"] - previous_at)
        previous_tokens, previous_at = advance["cumulative_tokens"], advance["at"]

    p50 = _quantile(token_gaps, 0.5)
    biggest = max(token_gaps)
    out.update({
        "p50_tokens_between_progress": p50,
        "p90_tokens_between_progress": _quantile(token_gaps, 0.9),
        "max_tokens_between_progress": biggest,
        "max_wall_between_progress_s": max(wall_gaps),
        "max_to_p50_ratio": (biggest / p50) if p50 else None,
        "tokens_after_last_progress": cumulative - previous_tokens,
        "wall_after_last_progress_s": span - previous_at,
        "content_to_progress_ratio": (
            out["content_revisions"] / out["progress_events"] if out["progress_events"] else None
        ),
    })
    return out, transitions


# The transitions that end a replan episode. rewrite is deliberately absent: a
# turn that keeps restating its steps after changing them has not yet answered
# whether the change led anywhere, which is the question an episode asks.
_EPISODE_OUTCOMES = ("advance", "terminal", "replan")


def replan_episodes(transitions, total_tokens, span):
    """Segment the transition stream at every replan.

    An episode runs from a replan to the first transition that resolves it, and
    the resolution is read from the transition's own kind — terminal is not
    folded into advance, because "the plan finished" and "the plan moved one
    step" are different answers to what a strategy change bought. An episode
    still open when the sample ends is censored, not dropped: a replan nothing
    ever followed is the case a survivor-only view would lose.
    """
    episodes = []
    for index, start in enumerate(transitions):
        if start["kind"] != "replan":
            continue
        resolution = next(
            (t for t in transitions[index + 1:] if t["kind"] in _EPISODE_OUTCOMES), None)
        if resolution is None:
            outcome, at, tokens = "end", span, total_tokens
        else:
            outcome = resolution["kind"]
            at, tokens = resolution["at"], resolution["cumulative_tokens"]
        episodes.append({
            "replan_at": start["at"], "replan_tokens": start["cumulative_tokens"],
            "outcome": outcome, "outcome_at": at, "outcome_tokens": tokens,
            "tokens_to_outcome": tokens - start["cumulative_tokens"],
            "wall_to_outcome": at - start["at"],
        })
    return episodes


def _million(value):
    return "-" if value is None else f"{value / 1e6:.2f}M"


def render(summary):
    lines = [
        f"source                      {summary['progress_source']} ({summary['progress_fidelity']})",
        f"total_tokens                {_million(summary['total_tokens'])}",
        f"wall_total                  {summary['wall_total_s'] / 3600:.2f}h",
        f"progress_events             {summary['progress_events']}",
        f"p50_tokens_between_progress {_million(summary['p50_tokens_between_progress'])}",
        f"p90_tokens_between_progress {_million(summary['p90_tokens_between_progress'])}",
        f"max_tokens_between_progress {_million(summary['max_tokens_between_progress'])}",
        f"tokens_after_last_progress  {_million(summary['tokens_after_last_progress'])}",
        f"content_revisions           {summary['content_revisions']}",
        f"plan_revisions              {summary['plan_revisions']}",
        f"progress_revisions          {summary['progress_revisions']}",
    ]
    if summary["max_wall_between_progress_s"] is not None:
        lines.append(f"max_wall_between_progress   {summary['max_wall_between_progress_s'] / 60:.1f}min")
    if summary["max_to_p50_ratio"] is not None:
        lines.append(f"max_to_p50_ratio            {summary['max_to_p50_ratio']:.0f}x")
    if summary["content_to_progress_ratio"] is not None:
        lines.append(f"content_to_progress_ratio   {summary['content_to_progress_ratio']:.1f}")
    return "\n".join(lines)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("trajectory", help="a trajectory export (JSON)")
    parser.add_argument("--json", action="store_true", help="emit the summary as JSON")
    parser.add_argument("--transitions", metavar="PATH",
                        help="write one JSON object per transition (no tool payloads, no source)")
    parser.add_argument("--require-semantic", action="store_true",
                        help="fail instead of falling back to the complete_step proxy")
    parser.add_argument("--replan-episodes", metavar="PATH",
                        help="write one JSON object per replan episode; semantic samples only")
    args = parser.parse_args(argv)

    try:
        summary, transitions = extract(args.trajectory)
    except TrajectoryError as err:
        print(f"error: {err}", file=sys.stderr)
        return 2

    if args.require_semantic and summary["progress_fidelity"] != "semantic":
        print("error: semantic todo_progress frames are absent; the complete_step "
              "fallback is available only in proxy mode", file=sys.stderr)
        return 2

    if args.replan_episodes and summary["progress_fidelity"] != "semantic":
        print("error: replan episodes need semantic transitions; the complete_step "
              "proxy carries no replan verdict to segment on", file=sys.stderr)
        return 2

    if args.replan_episodes:
        episodes = replan_episodes(transitions, summary["total_tokens"], summary["wall_total_s"])
        with open(args.replan_episodes, "w", encoding="utf-8") as fh:
            for episode in episodes:
                fh.write(json.dumps(episode, sort_keys=True) + "\n")

    if args.transitions:
        with open(args.transitions, "w", encoding="utf-8") as fh:
            for transition in transitions:
                fh.write(json.dumps(transition, sort_keys=True) + "\n")

    print(json.dumps(summary, indent=2, sort_keys=True) if args.json else render(summary))
    return 0


if __name__ == "__main__":
    sys.exit(main())
