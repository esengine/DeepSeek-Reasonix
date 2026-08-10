def pages(items, size):
    full = len(items) // size
    return [items[i * size:(i + 1) * size] for i in range(full)]
