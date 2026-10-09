import logging

log = logging.getLogger("orders")

def place(order, request_id):
    log.info("placing order %s", order["id"], extra={"request_id": request_id})
    return {"ok": True}
