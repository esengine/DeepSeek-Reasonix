import timings

def is_slow(entry):
    return entry["elapsed_ms"] > timings.SLOW_MS
