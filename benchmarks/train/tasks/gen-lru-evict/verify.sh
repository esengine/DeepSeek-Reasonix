#!/usr/bin/env bash
# Grader for the LRU cache benchmark.
# Runs from a directory whose top level holds the submitted files
# (i.e. workdir/ contents copied to the top level).
set -e

cd "$(dirname "$0")"

python3 - <<'PY'
import os
import sys

sys.path.insert(0, os.getcwd())

import lru_cache


def check(cond, msg):
    if not cond:
        raise AssertionError("FAIL: " + msg)


# --- basic get/put behavior ---
c = lru_cache.LRUCache(2)
check(c.get("missing") == -1, "get() of an absent key must return -1")

c.put("a", 1)
c.put("b", 2)
check(c.get("a") == 1, "get() must return the stored value")
check(c.get("b") == 2, "get() must return the stored value")

# --- eviction with no reads: oldest inserted entry goes first ---
c = lru_cache.LRUCache(2)
c.put("a", 1)
c.put("b", 2)
c.put("c", 3)
check(c.get("a") == -1, "with no reads, the oldest inserted entry must be evicted")
check(c.get("b") == 2, "b must still be cached")
check(c.get("c") == 3, "c must still be cached")

# --- reading a key must make it most recently used ---
c = lru_cache.LRUCache(2)
c.put("a", 1)
c.put("b", 2)
check(c.get("a") == 1, "reading a must refresh its recency")
c.put("c", 3)  # cache is full: must evict b, not the just-read a
check(c.get("b") == -1, "b is least recently used and must be evicted")
check(c.get("a") == 1, "the just-read a must survive eviction")
check(c.get("c") == 3, "the new key c must be cached")

# --- repeated reads keep an entry alive across evictions ---
c = lru_cache.LRUCache(3)
c.put("a", 1)
c.put("b", 2)
c.put("c", 3)
check(c.get("b") == 2, "reading b must refresh its recency")
c.put("d", 4)  # evicts a
check(c.get("a") == -1, "a is least recently used and must be evicted")
check(c.get("b") == 2 and c.get("c") == 3 and c.get("d") == 4,
      "b, c and d must all be cached after the first eviction")
check(c.get("b") == 2, "reading b again must refresh its recency")
c.put("e", 5)  # must evict c, not the just-read b
check(c.get("c") == -1, "c is least recently used and must be evicted")
check(c.get("b") == 2, "the just-read b must survive eviction")
check(c.get("d") == 4 and c.get("e") == 5, "d and e must be cached")

# --- put() on an existing key updates the value and refreshes recency ---
c = lru_cache.LRUCache(2)
c.put("a", 1)
c.put("b", 2)
c.put("a", 10)
c.put("c", 3)  # must evict b
check(c.get("b") == -1, "updated key a is recent; b must be evicted")
check(c.get("a") == 10, "put() on an existing key must update its value")
check(c.get("c") == 3, "the new key c must be cached")

print("all checks passed")
PY
