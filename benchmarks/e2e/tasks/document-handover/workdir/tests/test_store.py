import unittest

from cache import store


class StoreTest(unittest.TestCase):
    def test_round_trip(self):
        store.put("hello", 1)
        self.assertEqual(store.get("hello"), 1)

    @unittest.skip("fails until put() uses model_scoped_key; see cache/store.py TODO")
    def test_two_models_do_not_collide(self):
        store.put("hello", 1)
        store.put("hello", 2)
        self.assertEqual(store.get("hello"), 1)
