import json
import sys

def upgrade(row):
    parts = row["fullname"].rsplit(" ", 1)
    out = {k: v for k, v in row.items() if k != "fullname"}
    out["first"] = parts[0]
    out["last"] = parts[1] if len(parts) > 1 else ""
    return out

def main():
    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        print(json.dumps(upgrade(json.loads(line))))
