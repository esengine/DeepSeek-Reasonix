def delay_ms(base, attempt, cap):
    delay = base
    for _ in range(attempt):
        delay *= 2
    return delay
