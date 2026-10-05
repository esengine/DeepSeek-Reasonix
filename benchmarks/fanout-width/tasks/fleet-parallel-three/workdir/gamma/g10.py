"""dispatch key carried by module g10."""

MARKER = "MORVANE-7715-10"


def apply(state):
    state["gamma"] = MARKER
    return state
