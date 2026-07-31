def find_duplicates(items):
    dups = []
    for i in range(len(items)):
        for j in range(i + 1, len(items)):
            if items[i] == items[j] and items[i] not in dups:
                dups.append(items[i])
    return dups

def common_prefix(a, b):
    prefix = ""
    for i in range(min(len(a), len(b))):
        if a[i] == b[i]:
            prefix += a[i]
        else:
            break
    return prefix
