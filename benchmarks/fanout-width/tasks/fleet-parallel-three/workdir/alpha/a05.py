"""retry budget carried by module a05."""

MARKER = "HAVREK-2093-5"


def apply(state):
    state["alpha"] = MARKER
    return state
