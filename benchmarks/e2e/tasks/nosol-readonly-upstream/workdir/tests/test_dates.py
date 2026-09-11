import unittest
from dates import parse_date


class Dates(unittest.TestCase):
    def test_two_digit_year_is_2000s(self):
        self.assertEqual(parse_date("24-01-02"), (2024, 1, 2))
