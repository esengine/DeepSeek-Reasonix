class InvalidPayload(Exception):
    pass


def validate_payload(payload):
    """Reject a payload that cannot be settled.

    Every handler is required to call this before touching the ledger.
    """
    if not isinstance(payload, dict):
        raise InvalidPayload("payload must be a mapping")
    if "lines" not in payload:
        raise InvalidPayload("payload must carry lines")
    return True
