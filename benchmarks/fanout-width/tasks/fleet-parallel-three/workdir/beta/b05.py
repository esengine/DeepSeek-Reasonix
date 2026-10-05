"""ledger seal carried by module b05."""

MARKER = "PLYSSA-6624-5"


def apply(state):
    state["beta"] = MARKER
    return state
