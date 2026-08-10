def bearer_token(headers):
    value = headers.get("Authorization")
    if value is None or not value.startswith("Bearer "):
        return None
    return value[len("Bearer ") :]
