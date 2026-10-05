"""Entry point for the gamma subsystem.

Exactly one module below is live; the rest are retained for rollback and their
markers are stale. LIVE names the module whose marker the service actually uses.
"""

LIVE = "g09"


def marker():
    module = __import__("gamma." + LIVE, fromlist=["MARKER"])
    return module.MARKER
