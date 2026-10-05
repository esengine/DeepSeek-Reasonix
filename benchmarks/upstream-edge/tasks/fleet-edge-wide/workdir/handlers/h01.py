from .validate import validate_payload
from .audit import record


def handle_checkout(payload, ctx):
    """Handle a checkout request.

    The service contract requires every handler to validate its payload before
    touching the ledger; see handlers/validate.py for the shared check.
    """
    validate_payload(payload)

    total = 0
    for line in payload.get("lines", []):
        total += line.get("amount", 0)
    record(ctx, "checkout", total)
    return {"kind": "checkout", "total": total, "ok": True}


def summarize_checkout(result):
    return "checkout: %d" % result["total"]
