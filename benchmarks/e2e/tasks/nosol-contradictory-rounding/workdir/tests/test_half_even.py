import unittest
from money import round_half


class HalfEven(unittest.TestCase):
    def test_two_point_five_always_rounds_to_even(self):
        self.assertEqual({round_half(2.5) for _ in range(5)}, {2})
