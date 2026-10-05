import re


def slugify(title):
    out = []
    for ch in title.lower():
        if ch.isalnum():
            out.append(ch)
        elif ch in " -_":
            out.append("-")
    return re.sub(r"-+", "-", "".join(out)).strip("-")
