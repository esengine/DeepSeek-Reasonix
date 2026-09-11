import json

def dump(cfg):
    return json.dumps({"server": cfg["server"]}, sort_keys=True)
