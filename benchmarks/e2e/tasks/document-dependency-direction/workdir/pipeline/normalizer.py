"""Normalizes rows. Uses the reader; must not know about the writer."""

from pipeline import reader


def normalized(path):
    return [row.strip().lower() for row in reader.read_rows(path)]
