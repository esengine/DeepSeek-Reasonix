def _normalize_name(name):
    return " ".join(name.strip().lower().split())


def org_key(name):
    return "org:" + _normalize_name(name)
