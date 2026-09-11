from collections import namedtuple

Stage = namedtuple("Stage", "name priority fn")

_REGISTRY = []


def register(name, priority, fn):
    _REGISTRY.append(Stage(name, priority, fn))


def stages():
    from pipeline import archive, emit, ingest, scrub  # noqa: F401

    return list(_REGISTRY)
