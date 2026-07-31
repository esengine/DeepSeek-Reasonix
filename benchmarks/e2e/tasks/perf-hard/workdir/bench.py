import sys, os, time
sys.path.insert(0, os.getcwd())
from optimized import find_duplicates, common_prefix

items = list(range(1, 50001)) * 2
t0 = time.time()
dups = find_duplicates(items)
dt = time.time() - t0
assert sorted(dups) == list(range(1, 50001)), f"wrong dups: {len(dups)}"
assert dt < 1.0, f"too slow: {dt:.2f}s"
assert common_prefix("hello world", "hello there") == "hello "
assert common_prefix("abc", "xyz") == ""
print(f"OK find_duplicates {dt:.2f}s, common_prefix correct")
