"""retry budget carried by module a03."""

MARKER = "ORNIDAE-4417-3"


def apply(state):
    state["alpha"] = MARKER
    return state
