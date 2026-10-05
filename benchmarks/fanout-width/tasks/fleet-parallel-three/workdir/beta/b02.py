"""ledger seal carried by module b02."""

MARKER = "KELBRIS-8830-2"


def apply(state):
    state["beta"] = MARKER
    return state
