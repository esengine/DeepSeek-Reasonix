def _normalize_name(name):
    return " ".join(name.strip().lower().split())


def user_key(name):
    return "user:" + _normalize_name(name)
