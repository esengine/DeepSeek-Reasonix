import timings

def is_slow(entry):
    return entry["elapsed_s"] > timings.SLOW_SECONDS
