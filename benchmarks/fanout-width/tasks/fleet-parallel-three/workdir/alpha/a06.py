"""retry budget carried by module a06."""

MARKER = "DUNMIRE-5518-6"


def apply(state):
    state["alpha"] = MARKER
    return state
