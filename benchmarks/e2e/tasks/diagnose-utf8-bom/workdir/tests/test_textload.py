import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from textload import load_text


class TestLoadText(unittest.TestCase):
    def test_plain_file(self):
        with tempfile.NamedTemporaryFile(mode="w", suffix=".txt", delete=False, encoding="utf-8") as f:
            f.write("hello")
            path = f.name
        self.assertEqual(load_text(path), "hello")

    def test_bom_file(self):
        with tempfile.NamedTemporaryFile(mode="wb", suffix=".txt", delete=False) as f:
            f.write(b"\xef\xbb\xbfhello")
            path = f.name
        self.assertEqual(load_text(path), "hello")


if __name__ == "__main__":
    unittest.main()
