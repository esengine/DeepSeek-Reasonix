import unittest
from schema import COLUMNS


class Schema(unittest.TestCase):
    def test_has_archived(self):
        self.assertIn("archived", COLUMNS)
