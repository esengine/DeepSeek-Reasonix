def p95(sorted_values):
    if not sorted_values:
        return 0
    return sorted_values[int(len(sorted_values) * 0.95)]

def rate(events, seconds):
    return len(events) / seconds
