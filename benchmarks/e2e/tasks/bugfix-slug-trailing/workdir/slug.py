import re


def slugify(title):
    return re.sub(r"[^a-z0-9]+", "-", title.lower())
