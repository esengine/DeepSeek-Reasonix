KINDS = {"note", "task", "event"}


def handle_create(req):
    if not isinstance(req, dict):
        return ("error", "not a request")
    if "id" not in req:
        return ("error", "missing id")
    if not isinstance(req["id"], int) or req["id"] <= 0:
        return ("error", "bad id")
    if req.get("kind") not in KINDS:
        return ("error", "bad kind")
    return ("ok", f"created {req['kind']} {req['id']}")


def handle_update(req):
    if not isinstance(req, dict):
        return ("error", "not a request")
    if "id" not in req:
        return ("error", "missing id")
    if not isinstance(req["id"], int) or req["id"] <= 0:
        return ("error", "bad id")
    if req.get("kind") not in KINDS:
        return ("error", "bad kind")
    return ("ok", f"updated {req['kind']} {req['id']}")


def handle_delete(req):
    if not isinstance(req, dict):
        return ("error", "not a request")
    if "id" not in req:
        return ("error", "missing id")
    if not isinstance(req["id"], int) or req["id"] <= 0:
        return ("error", "bad id")
    if req.get("kind") not in KINDS:
        return ("error", "bad kind")
    return ("ok", f"deleted {req['kind']} {req['id']}")
