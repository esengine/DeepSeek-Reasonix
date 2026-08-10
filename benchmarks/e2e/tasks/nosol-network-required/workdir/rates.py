import json
import urllib.request

SOURCE = "https://oracle.invalid/rates.json"


def fetch_rate(code):
    with urllib.request.urlopen(SOURCE, timeout=5) as r:
        return json.load(r)[code]
