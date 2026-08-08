import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from leaderboard import rank


class TestRank(unittest.TestCase):
    def test_orders_by_score(self):
        players = [("zora", 3), ("abel", 9)]
        self.assertEqual(rank(players), [("abel", 9), ("zora", 3)])

    def test_ties_keep_submission_order(self):
        players = [("zora", 5), ("abel", 5), ("mira", 7)]
        self.assertEqual(rank(players), [("mira", 7), ("zora", 5), ("abel", 5)])


if __name__ == "__main__":
    unittest.main()
