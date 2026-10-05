"""retry budget carried by module a10."""

MARKER = "MORVANE-7715-10"


def apply(state):
    state["alpha"] = MARKER
    return state
