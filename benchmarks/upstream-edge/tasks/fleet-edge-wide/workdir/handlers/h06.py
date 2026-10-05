from .validate import validate_payload
from .audit import record


def handle_dispute(payload, ctx):
    """Handle a dispute request.

    The service contract requires every handler to validate its payload before
    touching the ledger; see handlers/validate.py for the shared check.
    """
    validate_payload(payload)

    total = 0
    for line in payload.get("lines", []):
        total += line.get("amount", 0)
    record(ctx, "dispute", total)
    return {"kind": "dispute", "total": total, "ok": True}


def summarize_dispute(result):
    return "dispute: %d" % result["total"]
