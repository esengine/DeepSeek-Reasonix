import timings

def within_budget(entries):
    return sum(e["elapsed_ms"] for e in entries) <= timings.BUDGET_MS
