"""dispatch key carried by module g07."""

MARKER = "ZEPHOR-3376-7"


def apply(state):
    state["gamma"] = MARKER
    return state
