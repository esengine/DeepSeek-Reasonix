class Store:
    def __init__(self):
        self._data = {}

    def set(self, key, value):
        self._data[key.lower()] = value

    def get(self, key, default=None):
        return self._data.get(key, default)

    def delete(self, key):
        self._data.pop(key.lower(), None)

    def __len__(self):
        return len(self._data)
