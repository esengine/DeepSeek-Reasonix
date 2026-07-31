import sys, os, tempfile
sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))
from src.app import login
from src.utils import handle_upload

def test_login_ok():
    users = {"alice": {"password": "hunter2"}}
    assert login("alice", "hunter2", users) is True

def test_login_wrong():
    users = {"alice": {"password": "hunter2"}}
    assert login("alice", "wrong", users) is False

def test_login_uses_constant_time():
    import inspect
    src = inspect.getsource(login)
    assert "compare_digest" in src, "login must use constant-time comparison"

def test_upload_rejects_traversal():
    with tempfile.TemporaryDirectory() as d:
        try:
            handle_upload("../../etc/passwd", b"x", d)
            raise AssertionError("path traversal must be rejected")
        except ValueError:
            pass

def test_upload_rejects_weird_chars():
    with tempfile.TemporaryDirectory() as d:
        try:
            handle_upload("a b/c.txt", b"x", d)
            raise AssertionError("spaces/slashes must be rejected")
        except ValueError:
            pass

if __name__ == "__main__":
    test_login_ok()
    test_login_wrong()
    test_login_uses_constant_time()
    test_upload_rejects_traversal()
    test_upload_rejects_weird_chars()
    print("ALL SECURITY TESTS PASS")
