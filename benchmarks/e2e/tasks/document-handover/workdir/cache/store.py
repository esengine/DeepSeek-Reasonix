"""Response cache. Mid-migration: the key format is being made model-aware."""

_ENTRIES = {}


def legacy_key(prompt):
    """Old key. Collides across models, which is the bug being fixed."""
    return prompt.strip()


def get(prompt):
    return _ENTRIES.get(legacy_key(prompt))


def put(prompt, value):
    # TODO: switch to cache.keys.model_scoped_key once callers pass the model.
    _ENTRIES[legacy_key(prompt)] = value
