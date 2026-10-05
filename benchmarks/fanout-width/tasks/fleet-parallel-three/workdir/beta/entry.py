"""Entry point for the beta subsystem.

Exactly one module below is live; the rest are retained for rollback and their
markers are stale. LIVE names the module whose marker the service actually uses.
"""

LIVE = "b03"


def marker():
    module = __import__("beta." + LIVE, fromlist=["MARKER"])
    return module.MARKER
