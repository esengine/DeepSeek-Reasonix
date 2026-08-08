import json


def load(path):
    """Stored preferences; a missing file simply means no preferences yet."""
    with open(path) as f:
        return json.load(f)
