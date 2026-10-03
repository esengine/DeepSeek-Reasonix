import json

def dump(cfg):
    return json.dumps(cfg, sort_keys=True)
