import json
import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from prefs import load


class TestPrefs(unittest.TestCase):
    def test_existing_file(self):
        with tempfile.NamedTemporaryFile(mode="w", suffix=".json", delete=False) as f:
            json.dump({"theme": "dark"}, f)
            path = f.name
        self.assertEqual(load(path), {"theme": "dark"})

    def test_missing_file_means_empty(self):
        self.assertEqual(load(os.path.join(tempfile.mkdtemp(), "nope.json")), {})


if __name__ == "__main__":
    unittest.main()
