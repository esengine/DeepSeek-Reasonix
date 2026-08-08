def head(lst, n):
    return lst[:n]


def tail(lst, n):
    if n <= 0:
        return []
    return lst[-n:-1]


def chunk(lst, size):
    return [lst[i : i + size] for i in range(0, len(lst), size)]
