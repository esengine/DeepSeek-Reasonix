import importlib.util
import pathlib
import unittest


BRIDGE_PATH = pathlib.Path(__file__).with_name("bridge.py")
SPEC = importlib.util.spec_from_file_location("intelifar_semantica_bridge", BRIDGE_PATH)
BRIDGE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(BRIDGE)


class BridgeIntegrationTest(unittest.TestCase):
    def test_locked_semantica_runs_duplicate_conflict_and_provenance_checks(self):
        result = BRIDGE.enrich([
            {
                "id": "IP-REAL-A1",
                "title": "企业知识中台",
                "type": "技术方案",
                "summary": "知识治理平台",
                "owner": "产品部",
                "sensitivity": "内部",
                "confidence": 0.90,
                "tags": ["知识治理"],
                "document": {"sourceName": "a.pdf"},
                "evidence": [{"id": "EV-A1", "section": "第一章", "locator": "P-1"}],
            },
            {
                "id": "IP-REAL-A2",
                "title": "企业知识中台",
                "type": "技术方案",
                "summary": "知识治理平台",
                "owner": "研发部",
                "sensitivity": "内部",
                "confidence": 0.95,
                "tags": ["知识治理"],
                "document": {"sourceName": "b.pdf"},
                "evidence": [],
            },
        ])
        self.assertEqual(result["version"], "0.6.0")
        self.assertEqual(result["checkedAssets"], 2)
        self.assertEqual(result["duplicates"][0]["assetIds"], ["IP-REAL-A1", "IP-REAL-A2"])
        self.assertEqual(result["duplicates"][0]["reasons"][0], "标题完全一致")
        self.assertEqual(result["conflicts"][0]["field"], "owner")
        self.assertEqual(set(result["conflicts"][0]["values"]), {"产品部", "研发部"})
        self.assertEqual(result["provenance"]["evidence"], 1)
        self.assertEqual(len(result["provenance"]["entries"][0]["checksum"]), 64)


if __name__ == "__main__":
    unittest.main()
