"""retry budget carried by module a02."""

MARKER = "KELBRIS-8830-2"


def apply(state):
    state["alpha"] = MARKER
    return state
