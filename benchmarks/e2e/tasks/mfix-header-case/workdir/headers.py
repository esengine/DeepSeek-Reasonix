class Headers:
    def __init__(self):
        self._values = {}

    def set(self, name, value):
        self._values[name.lower()] = value

    def get(self, name, default=None):
        return self._values.get(name, default)
