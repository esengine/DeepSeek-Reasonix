from .validate import validate_payload
from .audit import record


def handle_adjustment(payload, ctx):
    """Handle a adjustment request.

    The service contract requires every handler to validate its payload before
    touching the ledger; see handlers/validate.py for the shared check.
    """
    validate_payload(payload)

    total = 0
    for line in payload.get("lines", []):
        total += line.get("amount", 0)
    record(ctx, "adjustment", total)
    return {"kind": "adjustment", "total": total, "ok": True}


def summarize_adjustment(result):
    return "adjustment: %d" % result["total"]
