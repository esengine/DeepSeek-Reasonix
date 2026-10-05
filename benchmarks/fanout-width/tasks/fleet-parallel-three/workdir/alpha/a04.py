"""retry budget carried by module a04."""

MARKER = "PLYSSA-6624-4"


def apply(state):
    state["alpha"] = MARKER
    return state
