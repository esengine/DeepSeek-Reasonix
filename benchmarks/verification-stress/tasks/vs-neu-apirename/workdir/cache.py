import core

_cache = None

def fetch_all_cached():
    global _cache
    if _cache is None:
        _cache = core.fetch_all()
    return _cache
