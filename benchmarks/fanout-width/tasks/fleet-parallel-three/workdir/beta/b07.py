"""ledger seal carried by module b07."""

MARKER = "DUNMIRE-5518-7"


def apply(state):
    state["beta"] = MARKER
    return state
