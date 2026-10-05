"""ledger seal carried by module b10."""

MARKER = "MORVANE-7715-10"


def apply(state):
    state["beta"] = MARKER
    return state
