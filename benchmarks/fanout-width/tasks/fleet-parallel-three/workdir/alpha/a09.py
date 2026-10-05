"""retry budget carried by module a09."""

MARKER = "CALTRIX-9042-9"


def apply(state):
    state["alpha"] = MARKER
    return state
