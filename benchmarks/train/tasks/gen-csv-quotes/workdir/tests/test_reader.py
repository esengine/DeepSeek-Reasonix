"""Behavioural tests for the CSV reader (stdlib unittest only)."""
import unittest

from csv_reader import parse_csv


class ParseCsvTest(unittest.TestCase):
    def test_plain_rows(self):
        self.assertEqual(parse_csv("a,b\nc,d"), [["a", "b"], ["c", "d"]])

    def test_quoted_field_with_separator(self):
        self.assertEqual(parse_csv('a,"b,c",d'), [["a", "b,c", "d"]])

    def test_quoted_field_with_newline(self):
        self.assertEqual(parse_csv('a,"b\nc",d'), [["a", "b\nc", "d"]])

    def test_escaped_quotes(self):
        self.assertEqual(parse_csv('"say ""hi""",x'), [['say "hi"', "x"]])

    def test_empty_fields(self):
        self.assertEqual(parse_csv("a,,c"), [["a", "", "c"]])

    def test_trailing_newline(self):
        self.assertEqual(parse_csv("a,b\n"), [["a", "b"]])


if __name__ == "__main__":
    unittest.main()
