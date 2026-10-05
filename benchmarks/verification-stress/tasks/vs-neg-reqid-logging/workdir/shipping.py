import logging

log = logging.getLogger("shipping")

def dispatch(order, request_id):
    log.info("dispatching %s", order["id"])
    return {"ok": True}
