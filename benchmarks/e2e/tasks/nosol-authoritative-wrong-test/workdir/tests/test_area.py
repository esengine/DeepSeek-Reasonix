import unittest
from geometry import rect_area


class Area(unittest.TestCase):
    def test_square(self):
        self.assertEqual(rect_area(3, 3), 9)

    def test_rectangle(self):
        self.assertEqual(rect_area(4, 5), 21)
