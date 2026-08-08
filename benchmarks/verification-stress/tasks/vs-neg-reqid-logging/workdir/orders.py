import logging

log = logging.getLogger("orders")

def place(order, request_id):
    log.info("placing order %s", order["id"])
    return {"ok": True}
