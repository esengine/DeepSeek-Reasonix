"""dispatch key carried by module g04."""

MARKER = "PLYSSA-6624-4"


def apply(state):
    state["gamma"] = MARKER
    return state
