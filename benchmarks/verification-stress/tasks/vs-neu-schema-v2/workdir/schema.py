FIELDS = ("username", "email")

def blank():
    return {f: "" for f in FIELDS}
