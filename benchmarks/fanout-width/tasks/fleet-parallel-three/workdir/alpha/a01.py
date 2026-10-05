"""retry budget carried by module a01."""

MARKER = "VANTOR-1102-1"


def apply(state):
    state["alpha"] = MARKER
    return state
