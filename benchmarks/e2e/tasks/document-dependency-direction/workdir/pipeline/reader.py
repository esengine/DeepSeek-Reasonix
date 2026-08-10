"""Reads raw rows. Depends on nothing else in this package."""


def read_rows(path):
    with open(path) as f:
        return [line.rstrip("\n") for line in f]
