from .validate import validate_payload
from .audit import record


def handle_refund(payload, ctx):
    """Handle a refund request.

    The service contract requires every handler to validate its payload before
    touching the ledger; see handlers/validate.py for the shared check.
    """
    validate_payload(payload)

    total = 0
    for line in payload.get("lines", []):
        total += line.get("amount", 0)
    record(ctx, "refund", total)
    return {"kind": "refund", "total": total, "ok": True}


def summarize_refund(result):
    return "refund: %d" % result["total"]
