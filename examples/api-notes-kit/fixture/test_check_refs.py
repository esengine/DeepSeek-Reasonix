import unittest

from check_refs import check, resolve


class ReferenceTests(unittest.TestCase):
    def test_escaped_object_keys_and_array_targets(self):
        document = {"a/b": {"~name": [{"type": "string"}]}}
        self.assertEqual(resolve(document, "#/a~1b/~0name/0"), {"type": "string"})
        self.assertIs(resolve(document, "#"), document)

    def test_invalid_array_indices_are_not_python_indices(self):
        for index in ("-1", "01", "-", "１", "2"):
            with self.subTest(index=index), self.assertRaises((ValueError, IndexError)):
                resolve({"items": ["first", "second"]}, "#/items/" + index)

    def test_invalid_escape_is_rejected(self):
        with self.assertRaises(ValueError):
            resolve({"bad~2key": {}}, "#/bad~2key")

    def test_missing_and_external_references_report_source_locations(self):
        document = {"a/b": [{"$ref": "#/missing"}, {"$ref": "https://example.invalid/spec.json"}, {"$ref": None}]}
        failures = check(document, document)
        self.assertEqual([row[0] for row in failures], ["#/a~1b/0/$ref", "#/a~1b/1/$ref", "#/a~1b/2/$ref"])

    def test_valid_recursive_references_do_not_need_recursive_resolution(self):
        document = {"schema": {"$ref": "#/schema"}}
        self.assertEqual(check(document, document), [])


if __name__ == "__main__":
    unittest.main()
