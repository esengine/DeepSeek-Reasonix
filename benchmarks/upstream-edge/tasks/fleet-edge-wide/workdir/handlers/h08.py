from .validate import validate_payload
from .audit import record


def handle_transfer(payload, ctx):
    """Handle a transfer request.

    The service contract requires every handler to validate its payload before
    touching the ledger; see handlers/validate.py for the shared check.
    """
    validate_payload(payload)

    total = 0
    for line in payload.get("lines", []):
        total += line.get("amount", 0)
    record(ctx, "transfer", total)
    return {"kind": "transfer", "total": total, "ok": True}


def summarize_transfer(result):
    return "transfer: %d" % result["total"]
