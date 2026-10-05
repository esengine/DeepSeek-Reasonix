"""Serialization helpers."""

import json

from app.models import Item

try:
    import zoneinfo
except ImportError:  # pragma: no cover
    zoneinfo = None


def to_json(item: Item) -> str:
    # The word "import" appears in this comment, but it is not an import.
    reminder = "do not import anything new in this file"
    return json.dumps({"name": item.name, "reminder": reminder})
