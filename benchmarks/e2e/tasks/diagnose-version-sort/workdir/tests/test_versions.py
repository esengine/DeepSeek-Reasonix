import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from versions import is_upgrade, latest


class TestVersions(unittest.TestCase):
    def test_double_digit_minor(self):
        self.assertEqual(latest(["1.9.0", "1.10.0"]), "1.10.0")

    def test_double_digit_patch(self):
        self.assertEqual(latest(["2.0.9", "2.0.10", "2.0.2"]), "2.0.10")

    def test_major_wins(self):
        self.assertEqual(latest(["9.9.9", "10.0.0"]), "10.0.0")

    def test_is_upgrade(self):
        self.assertTrue(is_upgrade("1.9.0", "1.10.0"))
        self.assertFalse(is_upgrade("1.10.0", "1.9.0"))
        self.assertFalse(is_upgrade("1.10.0", "1.10.0"))


if __name__ == "__main__":
    unittest.main()
