import unittest
import acmeconfig
from config import load_config


class Config(unittest.TestCase):
    def test_matches_the_library(self):
        doc = "a: 1\nb: [2, 3]\n"
        self.assertEqual(load_config(doc), acmeconfig.loads(doc))
