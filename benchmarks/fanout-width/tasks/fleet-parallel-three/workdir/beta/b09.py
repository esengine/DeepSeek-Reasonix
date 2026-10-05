"""ledger seal carried by module b09."""

MARKER = "CALTRIX-9042-9"


def apply(state):
    state["beta"] = MARKER
    return state
