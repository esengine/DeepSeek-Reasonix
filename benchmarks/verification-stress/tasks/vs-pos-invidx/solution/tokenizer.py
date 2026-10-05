import re


def tokens(text):
    return re.findall(r"[a-z0-9]+", text.lower())
