import json


def write_report(path, numbers):
    total = sum(numbers)
    count = len(numbers)
    stats = {
        "count": count,
        "total": total,
        "mean": total / count if count else 0,
        "max": max(numbers) if numbers else None,
    }
    with open(path, "w") as f:
        json.dump(stats, f, sort_keys=True)
