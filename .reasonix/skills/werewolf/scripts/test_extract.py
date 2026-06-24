"""Tests for extract_name and extract_votes_from_responses.
Run: python3 test_extract.py
"""
import sys, os
sys.path.insert(0, os.path.dirname(__file__))
from utils import extract_name, extract_votes_from_responses

ok, fail = 0, 0
def t(name, result, expected):
    global ok, fail
    if result == expected:
        ok += 1; print(f"  PASS: {name}")
    else:
        fail += 1; print(f"  FAIL: {name} (got={result}, exp={expected})")

t("bold", extract_name("**张三**", ["张三","李四"]), "张三")
t("plain", extract_name("我投张三", ["张三","李四"]), "张三")
t("last", extract_name("赵六和吴十，投吴十", ["赵六","吴十"]), "吴十")
t("longest", extract_name("张三丰可疑", ["张三","张三丰"]), "张三丰")
t("no match", extract_name("我弃权", ["张三","李四"]), None)
t("plain2", extract_name("投李四一票", ["李四","王五"]), "李四")
# 动词前缀
t("kill", extract_name("刀张三", ["张三","李四"]), "张三")
t("check", extract_name("查李四", ["张三","李四"]), "李四")
t("guard", extract_name("守张三", ["张三","李四"]), "张三")
t("poison", extract_name("毒李四", ["张三","李四"]), "李四")
t("save", extract_name("救张三", ["张三","李四"]), "张三")
# Edges
t("empty input", extract_name("", ["张三"]), None)
t("symbols only", extract_name("@#$%", ["张三"]), None)
t("none responses", extract_votes_from_responses(None, ["张三"]), [])
# Vote extraction
t("voter not in list", extract_votes_from_responses(["X:投张三"], ["张三"]), [])
t("empty response", extract_votes_from_responses(["张三:"], ["张三"]), [])
t("no colon", extract_votes_from_responses(["张三投李四"], ["张三","李四"]), [])
t("skip_prefix", extract_votes_from_responses(["张三:投李四"], ["张三","李四"]), [("张三","李四")])
t("skip_prefix_no_colon", extract_name("张三投李四", ["张三","李四"], skip_prefix="张三"), "李四")
t("skip_prefix_with_colon", extract_name("张三:投李四", ["张三","李四"], skip_prefix="张三"), "李四")

print(f"\n{ok} passed, {fail} failed")
exit(0 if fail == 0 else 1)
