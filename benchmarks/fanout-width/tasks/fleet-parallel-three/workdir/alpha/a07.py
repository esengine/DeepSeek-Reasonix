"""retry budget carried by module a07."""

MARKER = "QORVEX-7741"


def apply(state):
    state["alpha"] = MARKER
    return state
