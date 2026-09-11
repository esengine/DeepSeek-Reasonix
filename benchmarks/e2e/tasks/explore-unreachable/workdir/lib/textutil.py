def squash(s):
    return " ".join(s.split())


def normalize(s):
    return squash(s).strip().lower()
