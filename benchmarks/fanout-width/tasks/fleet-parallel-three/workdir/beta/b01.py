"""ledger seal carried by module b01."""

MARKER = "VANTOR-1102-1"


def apply(state):
    state["beta"] = MARKER
    return state
