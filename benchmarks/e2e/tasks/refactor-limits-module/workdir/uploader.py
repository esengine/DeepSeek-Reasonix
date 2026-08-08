def plan_upload(size_bytes):
    if size_bytes > 8_388_608:
        return {"chunks": 0, "retries": 0, "rejected": True}
    chunk = 1_048_576
    chunks = (size_bytes + chunk - 1) // chunk
    return {"chunks": chunks, "retries": 3, "rejected": False}
