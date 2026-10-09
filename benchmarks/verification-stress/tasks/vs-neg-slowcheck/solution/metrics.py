import math

def p95(sorted_values):
    if not sorted_values:
        return 0
    return sorted_values[math.ceil(len(sorted_values) * 0.95) - 1]

def rate(events, seconds):
    if seconds <= 0:
        return 0.0
    return len(events) / seconds
