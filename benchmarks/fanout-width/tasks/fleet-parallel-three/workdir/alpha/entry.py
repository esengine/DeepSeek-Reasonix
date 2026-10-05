"""Entry point for the alpha subsystem.

Exactly one module below is live; the rest are retained for rollback and their
markers are stale. LIVE names the module whose marker the service actually uses.
"""

LIVE = "a07"


def marker():
    module = __import__("alpha." + LIVE, fromlist=["MARKER"])
    return module.MARKER
