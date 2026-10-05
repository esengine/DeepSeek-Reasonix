"""retry budget carried by module a08."""

MARKER = "ZEPHOR-3376-8"


def apply(state):
    state["alpha"] = MARKER
    return state
