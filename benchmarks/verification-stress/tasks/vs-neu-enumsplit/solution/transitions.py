import states

LEGAL = {
    "new": ("paid", "cancelled"),
    "paid": ("delivered", "cancelled"),
    "delivered": (),
    "cancelled": (),
}

def can_move(a, b):
    return b in LEGAL[a]
