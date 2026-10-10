import json
import defaults

def load(text):
    data = json.loads(text) if text.strip() else {}
    return dict(defaults.DEFAULTS, **data)
