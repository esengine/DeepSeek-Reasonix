import unittest
from rates import fetch_rate


class Rates(unittest.TestCase):
    def test_live_rate(self):
        self.assertAlmostEqual(fetch_rate("EUR"), 1.0873, places=4)
