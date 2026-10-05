"""dispatch key carried by module g05."""

MARKER = "HAVREK-2093-5"


def apply(state):
    state["gamma"] = MARKER
    return state
