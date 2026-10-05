class Log:
    """See README.md; append writes "<seq> <kind> <name>" lines, seq from 1."""

    def __init__(self, path):
        self.path = path
        self._seq = 0

    def append(self, kind, name):
        self._seq += 1
        with open(self.path, "a") as f:
            f.write(f"{self._seq} {kind} {name}\n")

    def count(self):
        return self._seq
