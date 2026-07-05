#!/usr/bin/env python3
"""Print the slowest tests from Go's JSON test stream."""

import argparse
import json
import sys


def events_from(path):
    stream = sys.stdin if path == "-" else open(path, "r", encoding="utf-8")
    try:
        for line in stream:
            try:
                yield json.loads(line)
            except json.JSONDecodeError:
                continue
    finally:
        if stream is not sys.stdin:
            stream.close()


def main():
    parser = argparse.ArgumentParser(
        description="Rank test cases from `go test -json` output by elapsed seconds."
    )
    parser.add_argument("json_file", nargs="?", default="-", help="JSON stream file, or stdin")
    parser.add_argument("-n", "--limit", type=int, default=20, help="number of rows to print")
    parser.add_argument("--min", type=float, default=0.0, help="minimum elapsed seconds")
    args = parser.parse_args()

    rows = []
    for event in events_from(args.json_file):
        if event.get("Action") not in {"pass", "fail", "skip"}:
            continue
        test = event.get("Test")
        elapsed = event.get("Elapsed")
        if not test or elapsed is None or elapsed < args.min:
            continue
        package = event.get("Package", "")
        name = f"{package}.{test}" if package else test
        rows.append((float(elapsed), event["Action"], name))

    rows.sort(key=lambda row: row[0], reverse=True)
    if not rows:
        print("No test timing rows found.")
        return

    print(f"{'SECONDS':>8}  {'STATE':<5}  TEST")
    for elapsed, action, name in rows[: args.limit]:
        print(f"{elapsed:8.3f}  {action:<5}  {name}")


if __name__ == "__main__":
    main()
