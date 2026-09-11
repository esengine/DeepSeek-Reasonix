import csv
import io
import pathlib
import unittest
from export import export


class Export(unittest.TestCase):
    def test_matches_golden(self):
        want = pathlib.Path("tests/golden/export.bin").read_bytes()
        self.assertEqual(export(), want)

    def test_output_is_valid_csv(self):
        rows = list(csv.reader(io.StringIO(export().decode("utf-8"))))
        self.assertEqual(rows[0], ["id", "name"])
