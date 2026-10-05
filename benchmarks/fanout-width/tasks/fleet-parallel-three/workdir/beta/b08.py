"""ledger seal carried by module b08."""

MARKER = "ZEPHOR-3376-8"


def apply(state):
    state["beta"] = MARKER
    return state
