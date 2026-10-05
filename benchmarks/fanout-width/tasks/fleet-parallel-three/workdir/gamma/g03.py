"""dispatch key carried by module g03."""

MARKER = "ORNIDAE-4417-3"


def apply(state):
    state["gamma"] = MARKER
    return state
