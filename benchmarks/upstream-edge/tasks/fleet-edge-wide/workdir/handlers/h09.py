from .validate import validate_payload
from .audit import record


def handle_reversal(payload, ctx):
    """Handle a reversal request.

    The service contract requires every handler to validate its payload before
    touching the ledger; see handlers/validate.py for the shared check.
    """
    validate_payload(payload)

    total = 0
    for line in payload.get("lines", []):
        total += line.get("amount", 0)
    record(ctx, "reversal", total)
    return {"kind": "reversal", "total": total, "ok": True}


def summarize_reversal(result):
    return "reversal: %d" % result["total"]
