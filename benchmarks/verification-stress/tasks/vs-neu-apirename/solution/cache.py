import core

_cache = None

def list_records_cached():
    global _cache
    if _cache is None:
        _cache = core.list_records()
    return _cache
