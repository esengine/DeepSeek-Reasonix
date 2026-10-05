"""A fixed-capacity LRU (least recently used) cache.

`get(key)` returns the cached value, or -1 when the key is not cached.
`put(key, value)` stores a value, updating it when the key is already
cached. Once the cache is at capacity, inserting a new key evicts the
least recently used entry.
"""


class LRUCache:
    def __init__(self, capacity):
        self.capacity = capacity
        self.data = {}
        self.order = []  # keys ordered from least to most recently used

    def _touch(self, key):
        """Move key to the end of the order list (most recently used)."""
        if key in self.order:
            self.order.remove(key)
        self.order.append(key)

    def get(self, key):
        if key not in self.data:
            return -1
        self._touch(key)
        return self.data[key]

    def put(self, key, value):
        if key in self.data:
            self.data[key] = value
            self._touch(key)
            return
        if len(self.data) >= self.capacity:
            victim = self.order.pop(0)
            del self.data[victim]
        self.data[key] = value
        self._touch(key)
