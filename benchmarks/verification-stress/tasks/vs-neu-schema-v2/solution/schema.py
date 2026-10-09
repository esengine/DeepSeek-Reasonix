FIELDS = ("handle", "email")

def blank():
    return {f: "" for f in FIELDS}
