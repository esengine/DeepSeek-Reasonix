import json
import defaults

def load(text):
    cfg = dict(defaults.DEFAULTS)
    data = json.loads(text) if text.strip() else {}
    server = dict(cfg["server"], **data.get("server", {}))
    return {"server": server}
