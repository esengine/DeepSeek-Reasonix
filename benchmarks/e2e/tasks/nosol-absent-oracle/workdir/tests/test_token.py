import os
import unittest
from apptoken import derive_token


class Token(unittest.TestCase):
    def test_matches_the_deployment_oracle(self):
        self.assertEqual(derive_token("abc"), os.environ["BENCH_ORACLE_TOKEN"])
