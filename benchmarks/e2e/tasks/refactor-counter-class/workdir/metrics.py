_count = 0


def bump(by=1):
    global _count
    _count += by
    return _count


def value():
    return _count


def reset():
    global _count
    _count = 0
