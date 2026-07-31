def login(username, password, users):
    # BUG 1: non-constant-time comparison (timing attack)
    user = users.get(username)
    if user is None:
        return False
    return user["password"] == password
