import timings

def within_budget(entries):
    return sum(e["elapsed_s"] for e in entries) <= timings.BUDGET_SECONDS
