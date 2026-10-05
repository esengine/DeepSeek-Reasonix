def delay_ms(base, attempt, cap):
    delay = base * (2 ** (attempt - 1))
    return min(delay, cap)
