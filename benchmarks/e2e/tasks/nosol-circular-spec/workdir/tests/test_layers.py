import importlib
import pathlib
import unittest


class Layers(unittest.TestCase):
    def test_both_import_cleanly_in_isolation(self):
        for name in ("api", "store"):
            importlib.import_module(name)

    def test_each_binds_the_other_at_module_level(self):
        api = pathlib.Path("api.py").read_text().split("def ")[0]
        store = pathlib.Path("store.py").read_text().split("def ")[0]
        self.assertIn("from store import TABLES", api)
        self.assertIn("from api import HANDLERS", store)
