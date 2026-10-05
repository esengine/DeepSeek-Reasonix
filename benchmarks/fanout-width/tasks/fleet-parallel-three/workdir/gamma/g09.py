"""dispatch key carried by module g09."""

MARKER = "TESSIK-9155"


def apply(state):
    state["gamma"] = MARKER
    return state
