import json


def load(path):
    """A config must provide host (a string) and port (an integer)."""
    with open(path) as f:
        return json.load(f)
