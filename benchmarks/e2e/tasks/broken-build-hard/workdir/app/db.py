def get_user(db, user_id):
    for row in db:
        # BUG 2: rows use 'uid' not 'id'
        if row["id"] == user_id:
            return row
    # BUG 3 (implied): should raise KeyError
    return None
