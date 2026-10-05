"""dispatch key carried by module g02."""

MARKER = "KELBRIS-8830-2"


def apply(state):
    state["gamma"] = MARKER
    return state
