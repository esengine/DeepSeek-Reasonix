"""dispatch key carried by module g08."""

MARKER = "CALTRIX-9042-8"


def apply(state):
    state["gamma"] = MARKER
    return state
