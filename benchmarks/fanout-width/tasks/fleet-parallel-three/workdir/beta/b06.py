"""ledger seal carried by module b06."""

MARKER = "HAVREK-2093-6"


def apply(state):
    state["beta"] = MARKER
    return state
