"""dispatch key carried by module g06."""

MARKER = "DUNMIRE-5518-6"


def apply(state):
    state["gamma"] = MARKER
    return state
