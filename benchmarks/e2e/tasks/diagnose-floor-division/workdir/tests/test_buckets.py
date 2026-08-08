import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from buckets import bucket_of


class TestBuckets(unittest.TestCase):
    def test_positive(self):
        self.assertEqual(bucket_of(25, 10), 2)

    def test_exact_edge(self):
        self.assertEqual(bucket_of(30, 10), 3)

    def test_negative_floors_down(self):
        self.assertEqual(bucket_of(-1, 10), -1)
        self.assertEqual(bucket_of(-10, 10), -1)
        self.assertEqual(bucket_of(-11, 10), -2)


if __name__ == "__main__":
    unittest.main()
