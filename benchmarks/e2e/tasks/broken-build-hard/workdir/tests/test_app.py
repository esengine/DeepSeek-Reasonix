import sys, os
sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))
from app import User, AdminUser
from app.db import get_user

def test_greeting():
    u = User(name="Alice", email="a@x.com")
    assert u.greeting() == "Hello, Alice!"

def test_admin_role():
    a = AdminUser(name="Bob", email="b@x.com")
    assert a.role == "admin"
    assert isinstance(a, User)

def test_get_user_by_uid():
    db = [{"uid": 1, "name": "Alice"}, {"uid": 2, "name": "Bob"}]
    assert get_user(db, 2)["name"] == "Bob"

def test_missing_user_raises():
    db = [{"uid": 1, "name": "Alice"}]
    try:
        get_user(db, 99)
        raise AssertionError("should raise KeyError")
    except KeyError:
        pass

if __name__ == "__main__":
    test_greeting(); test_admin_role(); test_get_user_by_uid(); test_missing_user_raises()
    print("ALL BUILD TESTS PASS")
