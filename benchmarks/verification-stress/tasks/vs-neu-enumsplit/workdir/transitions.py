import states

LEGAL = {"new": ("paid",), "paid": ("closed",), "closed": ()}

def can_move(a, b):
    return b in LEGAL[a]
