from .validate import validate_payload
from .audit import record


def handle_settlement(payload, ctx):
    """Handle a settlement request.

    The service contract requires every handler to validate its payload before
    touching the ledger; see handlers/validate.py for the shared check.
    """

    total = 0
    for line in payload.get("lines", []):
        total += line.get("amount", 0)
    record(ctx, "settlement", total)
    return {"kind": "settlement", "total": total, "ok": True}


def summarize_settlement(result):
    return "settlement: %d" % result["total"]
