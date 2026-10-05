"""ledger seal carried by module b04."""

MARKER = "ORNIDAE-4417-4"


def apply(state):
    state["beta"] = MARKER
    return state
