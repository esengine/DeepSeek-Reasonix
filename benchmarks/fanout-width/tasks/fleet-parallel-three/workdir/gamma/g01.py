"""dispatch key carried by module g01."""

MARKER = "VANTOR-1102-1"


def apply(state):
    state["gamma"] = MARKER
    return state
