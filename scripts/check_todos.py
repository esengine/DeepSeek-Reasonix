#!/usr/bin/env python3
"""Scan source files for markers and report them.
Usage: python check_todos.py [--dir <path>] [--ext .go,.py,.js] [--max-report 20]
"""
import re, sys, os

MARKER_RE = re.compile(r"(?i)\b(TODO|FIXME|HACK|XXX|BUG)\b[:\s]*(.*)")

SKIP_DIRS = {".git", "node_modules", "vendor", ".cache", "dist", "build", ".reasonix"}

def scan_file(filepath):
    found = []
    try:
        with open(filepath, "r", encoding="utf-8", errors="replace") as f:
            for i, line in enumerate(f, 1):
                m = MARKER_RE.search(line)
                if m:
                    tag, note = m.group(1), m.group(2).strip()
                    rel = os.path.relpath(filepath)
                    found.append((rel, i, tag, note))
    except Exception:
        pass
    return found

def main():
    args = sys.argv[1:]
    scan_dir = os.getcwd()
    extensions = [".go", ".py", ".js", ".ts", ".tsx", ".rs", ".java", ".c", ".h", ".cpp", ".hpp"]
    max_report = 20

    i = 0
    while i < len(args):
        if args[i] == "--dir" and i+1 < len(args):
            scan_dir = args[i+1]
            i += 2
        elif args[i] == "--ext" and i+1 < len(args):
            extensions = [x.strip() for x in args[i+1].split(",")]
            i += 2
        elif args[i] == "--max-report" and i+1 < len(args):
            max_report = int(args[i+1])
            i += 2
        elif args[i] in ("-h", "--help"):
            print(f"Usage: {sys.argv[0]} [--dir <path>] [--ext .go,.py,.js] [--max-report 20]")
            sys.exit(0)
        else:
            i += 1

    all_found = []
    for root, dirs, files in os.walk(scan_dir):
        dirs[:] = [d for d in dirs if d not in SKIP_DIRS and not d.startswith(".")]
        for f in files:
            ext = os.path.splitext(f)[1].lower()
            if ext in extensions:
                fp = os.path.join(root, f)
                all_found.extend(scan_file(fp))

    all_found.sort(key=lambda x: (x[0], x[1]))

    if not all_found:
        print("OK: no markers found")
        return

    total = len(all_found)
    report = all_found[:max_report]

    print(f"FOUND: {total} marker(s)\n")
    for fp, line_num, tag, note in report:
        print(f"  {fp}:{line_num} [{tag}] {note}")

    if total > max_report:
        print(f"\n  ... and {total - max_report} more (use --max-report to increase)")

    print(f"\nTotal: {total} marker(s) in {len(set(x[0] for x in all_found))} file(s)")
    sys.exit(1)

if __name__ == "__main__":
    main()
