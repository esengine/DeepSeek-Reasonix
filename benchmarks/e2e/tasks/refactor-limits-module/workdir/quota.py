def attempts_left(used):
    return max(0, 3 - used)


def upload_allowed(size_bytes):
    return 0 < size_bytes <= 8_388_608
