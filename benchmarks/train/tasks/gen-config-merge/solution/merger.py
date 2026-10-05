"""Configuration merging for three call sites.

merger.py historically grew one copy of the nested-dict merge logic per
call site: merge_config (application config defaults), merge_settings
(user settings), and merge_profile (user profile). All three now delegate
to the single shared helper ``_deep_merge``. Each public function returns a
new merged dict and must not mutate either input.

Merging rules (identical for all three functions):

* nested dicts are merged recursively when both the base value and the
  override value are dicts;
* on conflict the override wins — including when the override value is
  ``None`` or ``0``;
* a non-dict override replaces the base value entirely;
* a key present only in the override is added.
"""


def _deep_merge(base, override):
    """Return a new dict: ``override`` deep-merged over ``base``."""
    merged = dict(base)
    for key, value in override.items():
        if key in merged and isinstance(merged[key], dict) and isinstance(value, dict):
            merged[key] = _deep_merge(merged[key], value)
        else:
            merged[key] = value
    return merged


def merge_config(base, override):
    """Merge ``override`` into a copy of ``base`` and return the result."""
    return _deep_merge(base, override)


def merge_settings(base, override):
    """Merge ``override`` into a copy of ``base`` and return the result."""
    return _deep_merge(base, override)


def merge_profile(base, override):
    """Merge ``override`` into a copy of ``base`` and return the result."""
    return _deep_merge(base, override)
