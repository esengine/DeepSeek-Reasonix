"""ledger seal carried by module b03."""

MARKER = "MIRALD-3208"


def apply(state):
    state["beta"] = MARKER
    return state
