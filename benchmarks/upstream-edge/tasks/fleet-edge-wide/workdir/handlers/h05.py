from .validate import validate_payload
from .audit import record


def handle_payout(payload, ctx):
    """Handle a payout request.

    The service contract requires every handler to validate its payload before
    touching the ledger; see handlers/validate.py for the shared check.
    """
    validate_payload(payload)

    total = 0
    for line in payload.get("lines", []):
        total += line.get("amount", 0)
    record(ctx, "payout", total)
    return {"kind": "payout", "total": total, "ok": True}


def summarize_payout(result):
    return "payout: %d" % result["total"]
