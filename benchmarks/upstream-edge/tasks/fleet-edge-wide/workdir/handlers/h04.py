from .validate import validate_payload
from .audit import record


def handle_ledger(payload, ctx):
    """Handle a ledger request.

    The service contract requires every handler to validate its payload before
    touching the ledger; see handlers/validate.py for the shared check.
    """
    validate_payload(payload)

    total = 0
    for line in payload.get("lines", []):
        total += line.get("amount", 0)
    record(ctx, "ledger", total)
    return {"kind": "ledger", "total": total, "ok": True}


def summarize_ledger(result):
    return "ledger: %d" % result["total"]
