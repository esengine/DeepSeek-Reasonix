#!/usr/bin/env python3
"""Check all Markdown links in files under the given paths resolve to real files.
Usage: python scripts/link-checker.py [--dir <root>] <paths...>
"""
import re, os, sys
from urllib.parse import urlparse

LINK_RE = re.compile(r'\[([^\]]+)\]\(([^)]+)\)')
ANCHOR_RE = re.compile(r'^#')
SKIP_PREFIXES = ("http://", "https://", "mailto:", "ftp://")

def main():
    root = os.getcwd()
    targets = []

    args = sys.argv[1:]
    i = 0
    while i < len(args):
        if args[i] == "--dir" and i+1 < len(args):
            root = args[i+1]
            i += 2
        elif args[i] in ("-h", "--help"):
            print(f"Usage: {sys.argv[0]} [--dir <root>] <file.md> [file2.md ...]")
            sys.exit(0)
        else:
            targets.append(args[i])
            i += 1

    if not targets:
        # Default: scan docs/ and root *.md files
        for entry in os.scandir(root):
            if entry.name.endswith(".md") and entry.is_file():
                targets.append(entry.name)
        docs_dir = os.path.join(root, "docs")
        if os.path.isdir(docs_dir):
            for dirpath, _, files in os.walk(docs_dir):
                if ".git" in dirpath or "node_modules" in dirpath:
                    continue
                for f in files:
                    if f.endswith(".md"):
                        targets.append(os.path.relpath(os.path.join(dirpath, f), root))

    errors = []
    for target in targets:
        fp = os.path.join(root, target) if not os.path.isabs(target) else target
        if not os.path.exists(fp):
            errors.append(f"FILE NOT FOUND: {target}")
            continue
        try:
            with open(fp, "r", encoding="utf-8") as f:
                content = f.read()
        except Exception:
            continue

        for match in LINK_RE.finditer(content):
            link_text, url = match.group(1), match.group(2)
            url = url.strip()
            if not url or url.startswith(SKIP_PREFIXES) or ANCHOR_RE.match(url):
                continue
            # Resolve relative to the file's directory
            base_dir = os.path.dirname(fp)
            resolved = os.path.normpath(os.path.join(base_dir, url))
            if not os.path.exists(resolved):
                errors.append(f"BROKEN LINK: {target}:{content[:match.start()].count(chr(10))+1} — [{link_text}]({url}) → {resolved}")

    if errors:
        print(f"FAILED: {len(errors)} broken link(s)\n")
        for e in errors:
            print(f"  {e}")
        sys.exit(1)
    else:
        print(f"OK: checked {len(targets)} file(s), no broken links")

if __name__ == "__main__":
    main()
