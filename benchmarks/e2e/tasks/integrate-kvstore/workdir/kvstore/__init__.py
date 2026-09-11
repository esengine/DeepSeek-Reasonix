import json
import os


class KV:
    """See README.md; commits append one line to <path>.commits."""

    def __init__(self, path):
        self.path = path
        self._staged = None
        self._data = {}
        if os.path.exists(path):
            with open(path) as f:
                self._data = json.load(f)

    def begin(self):
        if self._staged is not None:
            raise RuntimeError("transaction already open")
        self._staged = {}

    def put(self, key, value):
        if self._staged is None:
            raise RuntimeError("put() outside a transaction; call begin() first")
        self._staged[key] = value

    def commit(self):
        if self._staged is None:
            raise RuntimeError("no open transaction")
        self._data.update(self._staged)
        self._staged = None
        tmp = self.path + ".tmp"
        with open(tmp, "w") as f:
            json.dump(self._data, f, sort_keys=True)
        os.replace(tmp, self.path)
        with open(self.path + ".commits", "a") as f:
            f.write("commit\n")

    def get(self, key):
        return self._data.get(key)
